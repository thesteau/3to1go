package runner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/3to1go/scout/internal/backup"
	"github.com/3to1go/scout/internal/config"
	"github.com/3to1go/scout/internal/services/state"
	"github.com/3to1go/shared/protocol"
)

func TestRememberedSizeApprovalStaysWithinJobAndDestination(t *testing.T) {
	settings := testRunnerSettings(t)
	settings.AutoApproveLargeUploads = true
	settings.StationURL = "https://station.example"
	job := backup.JobDefinition{RootPath: filepath.Join(settings.ScanRoot, "docs"), JobName: "docs"}
	approved := largeUploadApproval(&job, settings)
	for _, tc := range []struct {
		name           string
		change         func(*backup.JobDefinition, *config.Settings)
		want           bool
		stagedApproval bool
	}{
		{"same_job", func(*backup.JobDefinition, *config.Settings) {}, true, false},
		{"different_folder", func(j *backup.JobDefinition, _ *config.Settings) { j.RootPath += "-other" }, false, false},
		{"different_job_name", func(j *backup.JobDefinition, _ *config.Settings) { j.JobName = "other" }, false, false},
		{"different_scout", func(_ *backup.JobDefinition, s *config.Settings) { s.ScoutID = "other" }, false, false},
		{"different_station", func(_ *backup.JobDefinition, s *config.Settings) { s.StationURL = "https://other.example" }, false, false},
		{"staged_approval_new_station", func(_ *backup.JobDefinition, s *config.Settings) { s.StationURL = "https://other.example" }, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			currentJob, currentSettings := job, *settings
			tc.change(&currentJob, &currentSettings)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				var body protocol.UploadInitRequest
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.AllowOversized != tc.want {
					t.Errorf("allow_oversized = %v, want %v", body.AllowOversized, tc.want)
				}
				writeTestJSON(t, w, map[string]any{"status": "completed", "stored_as": "archive.tar.zst"})
			}))
			defer server.Close()
			runner := testRunner(t, &currentSettings, testUploadClient(server.URL))
			if err := runner.UploadApprovals.Save(context.Background(), approved); err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(t.TempDir(), "backup.tar.zst")
			if err := os.WriteFile(archive, []byte("archive"), 0o644); err != nil {
				t.Fatal(err)
			}
			st := state.JobState{JobName: currentJob.JobName, PendingArchive: archive, PendingFingerprint: "abcdef12", PendingTimestamp: "2026-10-13T00:00:00Z"}
			st.PendingUploadSizeApproved = tc.stagedApproval
			if !runner.uploadPendingArchive(&currentJob, &st, &currentSettings) {
				t.Fatal("test upload failed")
			}
		})
	}
}

func TestRemovingRememberedApprovalClearsStagedApprovalAndWaitsForActiveWork(t *testing.T) {
	settings := testRunnerSettings(t)
	runner := testRunner(t, settings, testUploadClient("http://example.invalid"))
	job := &backup.JobDefinition{RootPath: filepath.Join(settings.ScanRoot, "docs"), JobName: "docs"}
	a := largeUploadApproval(job, settings)
	if err := runner.UploadApprovals.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	st := state.JobState{PendingArchive: "staged.tar.zst", PendingUploadSizeApproved: true, LastSuccessfulFingerprint: "last-good"}
	if err := runner.StateStore.Set(job.RootPath, st); err != nil {
		t.Fatal(err)
	}
	runner.cycleLock.Lock()
	err := runner.RemoveLargeUploadApproval(context.Background(), a.ID)
	runner.cycleLock.Unlock()
	if err == nil {
		t.Fatal("approval removed while work was active")
	}
	if got, err := runner.UploadApprovals.Get(context.Background(), a.ID); err != nil || got == nil {
		t.Fatal("busy removal lost approval")
	}
	if err := runner.RemoveLargeUploadApproval(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	got := runner.StateStore.Get(job.RootPath)
	if got.PendingUploadSizeApproved || got.PendingArchive != st.PendingArchive || got.LastSuccessfulFingerprint != st.LastSuccessfulFingerprint {
		t.Fatalf("incorrect state after removal: %+v", got)
	}
}

func TestFailedApprovalSaveKeepsArchiveHeld(t *testing.T) {
	settings := testRunnerSettings(t)
	runner := testRunner(t, settings, testUploadClient("http://example.invalid"))
	runner.UploadApprovals.(*mockUploadApprovalStore).err = errors.New("database unavailable")
	job := &backup.JobDefinition{RootPath: filepath.Join(settings.ScanRoot, "docs"), JobName: "docs"}
	archive := filepath.Join(t.TempDir(), "backup.tar.zst")
	if err := os.WriteFile(archive, []byte("archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := state.JobState{PendingArchive: archive, PendingFingerprint: "abcdef12", PendingTimestamp: "2026-10-13T00:00:00Z", LastErrorCategory: protocol.StatusUploadApprovalRequired}
	runner.processJobLocked(job, &st, settings, true)
	if got := runner.StateStore.Get(job.RootPath); got.LastStatus != heldForReviewStatus || !got.ManualInterventionRequired || got.PendingUploadSizeApproved {
		t.Fatalf("failed save allowed upload: %+v", got)
	}
}
