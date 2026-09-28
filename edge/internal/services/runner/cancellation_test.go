package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/3to1go/edge/internal/backup"
	"github.com/3to1go/edge/internal/services/state"
)

func TestCancelStopsInFlightUploadAndKeepsStagedArchive(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		close(started)
		select {
		case <-req.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	settings := testRunnerSettings(t)
	r := testRunner(t, settings, testUploadClient(server.URL))
	job := &backup.JobDefinition{RootPath: filepath.Join(settings.ScanRoot, "job"), JobName: "job"}
	if err := os.MkdirAll(settings.SpoolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(settings.SpoolDir, "staged.tar.zst")
	if err := os.WriteFile(archive, []byte("archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := state.JobState{PendingArchive: archive, PendingFingerprint: "fp", PendingTimestamp: "now", PendingArchiveSHA256: "hash"}
	done := r.beginOperation(context.Background())
	defer done()
	finished := make(chan struct{})
	go func() { defer close(finished); r.uploadPendingArchive(job, &s, settings) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not start")
	}
	if !r.CancelOperation() {
		t.Fatal("no active operation")
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("upload did not cancel promptly")
	}
	if s.LastStatus != "cancelled" || s.NextRetryAt != "" || s.PendingArchive != archive {
		t.Fatalf("state=%+v", s)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatal("lost staged archive")
	}
}

func TestCancelledPreparationAndFreshOperation(t *testing.T) {
	settings := testRunnerSettings(t)
	r := testRunner(t, settings, nil)
	done := r.beginOperation(context.Background())
	r.CancelOperation()
	job := &backup.JobDefinition{RootPath: settings.ScanRoot, JobName: "job"}
	s := state.JobState{}
	r.processJobLocked(job, &s, settings, true)
	if s.LastStatus != "cancelled" {
		t.Fatalf("status=%s", s.LastStatus)
	}
	done()
	if r.CancelOperation() {
		t.Fatal("idle runner reports active cancellation")
	}
	done = r.beginOperation(context.Background())
	defer done()
	if err := r.operationContext().Err(); err != nil {
		t.Fatal("new operation inherited cancellation")
	}
}
