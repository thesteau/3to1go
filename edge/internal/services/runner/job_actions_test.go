package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/3to1go/edge/internal/backup"
	"github.com/3to1go/edge/internal/services/directories"
	"github.com/3to1go/edge/internal/services/state"
)

func TestClearStagedBackup(t *testing.T) {
	settings := testRunnerSettings(t)
	r := testRunner(t, settings, nil)
	r.DirService = directories.NewDirectoryService(settings, r.logger, r.StateStore)
	job := filepath.Join(settings.ScanRoot, "job")
	if err := os.MkdirAll(job, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := backup.WriteUploadDir(job, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(settings.SpoolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(settings.SpoolDir, "pending.tar.zst")
	if err := os.WriteFile(archive, []byte("archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(job, "source")
	if err := os.WriteFile(source, []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := state.JobState{PendingArchive: archive, PendingFingerprint: "pending", UploadID: "upload", UploadOffset: 42, UploadAttemptCount: 3, ManualInterventionRequired: true, NextRetryAt: "later", LastSuccessfulFingerprint: "previous", LastSuccessfulUpload: "yesterday", LastErrorDetail: "failed"}
	if err := r.StateStore.Set(job, s); err != nil {
		t.Fatal(err)
	}
	r.cycleLock.Lock()
	if err := r.ClearStagedBackup("job"); err == nil {
		t.Fatal("allowed clearing active cycle")
	}
	if r.RunCycle() {
		t.Fatal("cycle ran during exclusive operation")
	}
	r.cycleLock.Unlock()
	if _, err := os.Stat(archive); err != nil {
		t.Fatal("removed archive while busy")
	}
	if err := r.ClearStagedBackup("job"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("archive remains: %v", err)
	}
	got := r.StateStore.Get(job)
	if got.PendingArchive != "" || got.PendingFingerprint != "" || got.UploadID != "" || got.UploadOffset != 0 || got.UploadAttemptCount != 0 || got.ManualInterventionRequired || got.NextRetryAt != "" || got.LastErrorDetail != "" {
		t.Fatalf("state not cleared: %+v", got)
	}
	if got.LastSuccessfulFingerprint != "previous" || got.LastSuccessfulUpload != "yesterday" {
		t.Fatal("lost successful backup history")
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal("source changed")
	}
	// Already missing archives should be clearable, but paths outside spool are refused.
	s.PendingArchive = archive
	if err := r.StateStore.Set(job, s); err != nil {
		t.Fatal(err)
	}
	if err := r.ClearStagedBackup("job"); err != nil {
		t.Fatal(err)
	}
	s.PendingArchive = source
	if err := r.StateStore.Set(job, s); err != nil {
		t.Fatal(err)
	}
	if err := r.ClearStagedBackup("job"); err == nil {
		t.Fatal("allowed deletion outside spool")
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal("source deleted")
	}
}
