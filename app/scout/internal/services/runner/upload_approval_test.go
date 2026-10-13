package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/3to1go/scout/internal/backup"
	"github.com/3to1go/scout/internal/services/directories"
	"github.com/3to1go/shared/protocol"
)

func TestOversizedBackupApprovalLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name               string
		async, autoApprove bool
	}{
		{"synchronous_manual", false, false}, {"asynchronous_manual", true, false},
		{"synchronous_auto", false, true}, {"asynchronous_auto", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := testRunnerSettings(t)
			settings.AutoApproveLargeUploads = tc.autoApprove
			settings.KeepLocalPending = true
			settings.AnomalyMode = "off"
			jobRoot := filepath.Join(settings.ScanRoot, "docs")
			if err := os.MkdirAll(jobRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := backup.WriteUploadDir(jobRoot, map[string]any{"job_name": "docs"}); err != nil {
				t.Fatal(err)
			}
			dataPath := filepath.Join(jobRoot, "data.txt")
			if err := os.WriteFile(dataPath, []byte("payload"), 0o644); err != nil {
				t.Fatal(err)
			}

			var mu sync.Mutex
			var requests []protocol.UploadInitRequest
			failApproved := true
			chunks, uploads := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch req.URL.Path {
				case protocol.UploadInitiatePath:
					var body protocol.UploadInitRequest
					if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					requests = append(requests, body)
					if !body.AllowOversized {
						w.WriteHeader(http.StatusRequestEntityTooLarge)
						writeTestJSON(t, w, map[string]any{"detail": map[string]any{
							"status":  protocol.StatusUploadApprovalRequired,
							"message": "Archive is 3 MB, above Station's 1 MB upload approval threshold. Choose Upload anyway.",
						}})
						return
					}
					if failApproved {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					writeTestJSON(t, w, map[string]any{"upload_id": "u1", "status": "initiated", "next_offset": 0, "recommended_chunk_size_bytes": 1 << 20})
				case "/backup/uploads/u1/chunk":
					chunks++
					writeTestJSON(t, w, map[string]any{"next_offset": 1 << 40})
				case "/backup/uploads/u1/finalize":
					uploads++
					writeTestJSON(t, w, map[string]any{"status": "ok", "stored_as": "docs.tar.zst"})
				default:
					t.Errorf("unexpected path %s", req.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			client := testUploadClient(server.URL)
			client.SetHTTPClient(server.Client())
			runner := testRunner(t, settings, client)
			job, err := backup.LoadJobDefinition(jobRoot, filepath.Join(jobRoot, backup.UploadDirFilename), nil)
			if err != nil {
				t.Fatal(err)
			}
			cycle := func() {
				t.Helper()
				st := runner.StateStore.Get(jobRoot)
				runner.processJobLocked(job, &st, settings, false)
			}
			cycle()
			held := runner.StateStore.Get(jobRoot)
			if held.LastStatus != heldForReviewStatus || !held.ManualInterventionRequired || held.PendingArchive == "" || held.ActivePhase != "" || held.NextRetryAt != "" {
				t.Fatalf("not held for approval: %+v", held)
			}
			if held.PendingUploadSizeApproved || chunks != 0 || uploads != 0 || len(requests) != 1 {
				t.Fatal("archive uploaded or approved without operator review")
			}
			// Recreate the runner on its saved state; scheduled cycles retain the warning.
			restarted := testRunner(t, settings, client)
			restarted.StateStore, restarted.Anomalies = runner.StateStore, runner.Anomalies
			restarted.UploadApprovals = runner.UploadApprovals
			runner = restarted
			recorder := &recordingStateStore{jobStateStore: runner.StateStore}
			runner.StateStore = recorder
			cycle()
			// Every saved state must retain the reason even if the scan is interrupted.
			for _, saved := range recorder.saved {
				if saved.LastErrorCategory != held.LastErrorCategory || saved.LastErrorDetail != held.LastErrorDetail {
					t.Fatalf("an interrupted scan could lose the approval warning: %+v", saved)
				}
			}
			again := runner.StateStore.Get(jobRoot)
			if again.LastStatus != heldForReviewStatus || again.LastErrorDetail != held.LastErrorDetail || again.LastErrorCategory != protocol.StatusUploadApprovalRequired || again.ActivePhase != "" || len(requests) != 1 {
				t.Fatalf("hold lost after restart: %+v", again)
			}

			// Both operator endpoints approve the exact pending archive.
			if tc.async {
				runner.DirService = directories.NewDirectoryService(settings, runner.logger, runner.StateStore)
				_, err = runner.StartForceSendAsync("docs")
				runner.cycleLock.Lock()
				runner.cycleLock.Unlock()
			} else {
				_, err = runner.ForceSendJob(context.Background(), "docs")
			}
			if err != nil {
				t.Fatal(err)
			}
			approved := runner.StateStore.Get(jobRoot)
			if !approved.PendingUploadSizeApproved || approved.PendingArchive != held.PendingArchive || approved.LastStatus != "retry_scheduled" {
				t.Fatalf("approval did not survive network failure: %+v", approved)
			}
			mu.Lock()
			for _, request := range requests[1:] {
				if !request.AllowOversized || request.ArchiveSHA256 != requests[0].ArchiveSHA256 || request.IdempotencyKey != requests[0].IdempotencyKey {
					t.Error("approval changed the archive or omitted the override")
				}
			}
			failApproved = false
			mu.Unlock()
			// A restart and automatic retry keep this archive's approval.
			restarted = testRunner(t, settings, client)
			restarted.StateStore, restarted.Anomalies = runner.StateStore, runner.Anomalies
			restarted.UploadApprovals = runner.UploadApprovals
			runner = restarted
			approved.NextRetryAt = ""
			if err := runner.StateStore.Set(jobRoot, approved); err != nil {
				t.Fatal(err)
			}
			cycle()
			if st := runner.StateStore.Get(jobRoot); st.LastStatus != "success" || st.PendingUploadSizeApproved || uploads != 1 {
				t.Fatalf("approved retry failed: %+v", st)
			}
			// With auto-approval enabled, the job's next archive uses its first approval.
			if err := os.WriteFile(dataPath, []byte("larger payload"), 0o644); err != nil {
				t.Fatal(err)
			}
			cycle()
			wantStatus, wantUploads := heldForReviewStatus, 1
			if tc.autoApprove {
				wantStatus, wantUploads = "success", 2
			}
			if st := runner.StateStore.Get(jobRoot); st.LastStatus != wantStatus || st.PendingUploadSizeApproved || uploads != wantUploads || requests[len(requests)-1].AllowOversized != tc.autoApprove {
				t.Fatalf("next archive approval: %+v, uploads %d", st, uploads)
			}
			approvals, err := runner.LargeUploadApprovals(context.Background())
			if err != nil || len(approvals) != 1 {
				t.Fatalf("remembered approvals: %+v, %v", approvals, err)
			}
			if err := runner.RemoveLargeUploadApproval(context.Background(), approvals[0].ID); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dataPath, []byte("even larger payload"), 0o644); err != nil {
				t.Fatal(err)
			}
			cycle()
			if st := runner.StateStore.Get(jobRoot); st.LastStatus != heldForReviewStatus || uploads != wantUploads || requests[len(requests)-1].AllowOversized {
				t.Fatalf("removing approval did not restore the hold: %+v", st)
			}
			if list, err := runner.LargeUploadApprovals(context.Background()); err != nil || len(list) != 0 {
				t.Fatalf("approval still listed: %+v, %v", list, err)
			}
			// Restore the remembered approval, then toggle the setting off/on.
			if err := runner.UploadApprovals.Save(context.Background(), approvals[0]); err != nil {
				t.Fatal(err)
			}
			settings.AutoApproveLargeUploads = false
			cycle()
			if st := runner.StateStore.Get(jobRoot); st.LastStatus != heldForReviewStatus {
				t.Fatalf("disabled auto-approval uploaded a held archive: %+v", st)
			}
			settings.AutoApproveLargeUploads = true
			cycle()
			if st := runner.StateStore.Get(jobRoot); st.LastStatus != "success" || uploads != wantUploads+1 {
				t.Fatalf("re-enabled auto-approval did not upload: %+v", st)
			}
		})
	}
}
