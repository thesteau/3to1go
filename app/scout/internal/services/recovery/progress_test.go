package recovery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/3to1go/scout/internal/backup"
	"github.com/3to1go/scout/internal/encryption"
	"github.com/3to1go/scout/internal/transfer"
)

func TestRestoreProgressStreamsAndCompletesForBothRestoreKinds(t *testing.T) {
	for _, request := range []bool{false, true} {
		t.Run(fmt.Sprintf("request=%t", request), func(t *testing.T) {
			store, downloader := newMockStateStore(), &mockDownloader{}
			service := newTestRecoveryService(t, store, downloader)
			if got := service.Progress(); got.Status != "idle" {
				t.Fatalf("initial progress: %+v", got)
			}
			source := filepath.Join(t.TempDir(), "source")
			content := bytes.Repeat([]byte("original files\n"), 10000)
			if err := os.WriteFile(source, content, 0o640); err != nil {
				t.Fatal(err)
			}
			archive, sealed := filepath.Join(t.TempDir(), "archive.tar.zst"), filepath.Join(t.TempDir(), "sealed")
			if err := backup.CreateArchive(archive, []*backup.DiscoveredFile{{SourcePath: source, ArchivePath: "file.txt"}, {ArchivePath: "empty", IsDir: true}}); err != nil {
				t.Fatal(err)
			}
			if err := encryption.EncryptFile(service.encKey, archive, sealed); err != nil {
				t.Fatal(err)
			}
			payload, err := os.ReadFile(sealed)
			if err != nil {
				t.Fatal(err)
			}
			filename := "photos__2026-10-01T00-00-00Z__abcdef12.tar.zst"
			downloader.download = func(ctx context.Context, path string) (string, error) {
				reader := transfer.Observe(ctx, bytes.NewReader(payload), int64(len(payload)))
				first := make([]byte, len(payload)/2)
				if _, err := io.ReadFull(reader, first); err != nil {
					return "", err
				}
				p := service.Progress()
				if p.OperationID != "current-restore" || p.Status != "running" || p.Phase != "downloading" || p.Percent == nil || *p.Percent < 49 || *p.Percent > 50 {
					t.Errorf("partial download progress: %+v", p)
				}
				rest, err := io.ReadAll(reader)
				if err != nil {
					return "", err
				}
				return filename, os.WriteFile(path, append(first, rest...), 0o600)
			}
			ctx := WithOperationID(context.Background(), "current-restore")
			job := testJob(t.TempDir())
			if request {
				_, err = service.RecoverRequest(ctx, job, filename, service.encKey, downloader.download)
			} else {
				_, err = service.Recover(ctx, job, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			p := service.Progress()
			if p.Status != "completed" || p.Phase != "extracting" || p.Percent == nil || *p.Percent != 100 || p.RestoredFiles != 1 || p.BytesDone < int64(len(content)) {
				t.Fatalf("completed progress: %+v", p)
			}
			got, err := os.ReadFile(filepath.Join(job.RootPath, "file.txt"))
			if err != nil || !bytes.Equal(got, content) {
				t.Fatalf("restored file: %v", err)
			}
			if info, err := os.Stat(filepath.Join(job.RootPath, "empty")); err != nil || !info.IsDir() {
				t.Fatalf("empty folder: %v", err)
			}
			pending, err := os.ReadDir(service.settings.SpoolDir)
			if err != nil || len(pending) != 0 {
				t.Fatalf("temporary archives remain: %v", err)
			}
			if request && len(store.states) != 0 {
				t.Fatal("Station restore recorded job state")
			}
		})
	}
}

func TestRestoreSpaceFailureReportsMountAndFailedProgress(t *testing.T) {
	for _, request := range []bool{false, true} {
		service := newTestRecoveryService(t, newMockStateStore(), &mockDownloader{err: &os.PathError{Op: "write", Path: "download", Err: syscall.ENOSPC}})
		ctx, job := WithOperationID(context.Background(), "failed-restore"), testJob(t.TempDir())
		var err error
		if request {
			_, err = service.RecoverRequest(ctx, job, "exact.tar.zst", service.encKey, func(context.Context, string) (string, error) { return "", syscall.ENOSPC })
		} else {
			_, err = service.Recover(ctx, job, "")
		}
		var failure *RecoveryError
		if !errors.As(err, &failure) || failure.StatusCode != 507 || !strings.Contains(failure.Message, service.settings.SpoolDir) {
			t.Fatalf("space error: %v", err)
		}
		if p := service.Progress(); p.Status != "failed" || p.Phase != "downloading" || p.Percent != nil {
			t.Fatalf("failed progress: %+v", p)
		}
	}
}

func TestDecryptFilesystemFailureIsNotReportedAsWrongKey(t *testing.T) {
	service := newTestRecoveryService(t, newMockStateStore(), &mockDownloader{})
	err := service.decryptError(fmt.Errorf("decrypt: %w", &os.PathError{Op: "write", Path: "decrypted", Err: syscall.ENOSPC}), "wrong key")
	var failure *RecoveryError
	if !errors.As(err, &failure) || failure.StatusCode != 507 || strings.Contains(failure.Message, "wrong key") {
		t.Fatalf("disk full mistaken for key failure: %v", err)
	}
	if err := service.decryptError(context.Canceled, "wrong key"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestProgressRemainsReadableDuringABlockedDownload(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	downloader := &mockDownloader{download: func(context.Context, string) (string, error) {
		close(started)
		<-release
		return "", context.Canceled
	}}
	service := newTestRecoveryService(t, newMockStateStore(), downloader)
	finished := make(chan error, 1)
	job := testJob(t.TempDir())
	defer func() {
		close(release)
		select {
		case <-finished:
			if p := service.Progress(); p.Status != "failed" {
				t.Errorf("cancelled progress: %+v", p)
			}
		case <-time.After(2 * time.Second):
			t.Error("restore did not stop")
		}
	}()
	go func() {
		_, err := service.Recover(WithOperationID(context.Background(), "busy-restore"), job, "")
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("download never started")
	}
	read := make(chan Progress, 1)
	go func() { read <- service.Progress() }()
	select {
	case p := <-read:
		if p.Status != "running" || p.OperationID != "busy-restore" {
			t.Fatalf("progress: %+v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("progress blocked behind the download")
	}
}
