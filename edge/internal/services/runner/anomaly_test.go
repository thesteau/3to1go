package runner

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/3to1go/edge/internal/anomaly"
	"github.com/3to1go/edge/internal/backup"
	"github.com/3to1go/edge/internal/services/state"
)

var timeZero = time.Unix(0, 0)

func TestUnusualBackupIsHeldUntilForceUpload(t *testing.T) {
	settings := testRunnerSettings(t)
	settings.AnomalyMode = anomaly.ModeHold
	jobRoot := filepath.Join(settings.ScanRoot, "docs")
	if err := os.MkdirAll(jobRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := backup.WriteUploadDir(jobRoot, map[string]any{"job_name": "docs"}); err != nil {
		t.Fatal(err)
	}

	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/backup/uploads/initiate":
			writeTestJSON(t, w, map[string]any{"upload_id": "u1", "status": "initiated", "next_offset": 0, "recommended_chunk_size_bytes": 1 << 20})
		case "/backup/uploads/u1/chunk":
			writeTestJSON(t, w, map[string]any{"next_offset": 1 << 40})
		case "/backup/uploads/u1/finalize":
			uploads++
			writeTestJSON(t, w, map[string]any{"status": "ok", "stored_as": fmt.Sprintf("docs-%d.tar.zst", uploads)})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
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
		s := runner.StateStore.Get(jobRoot)
		runner.processJobLocked(job, &s, settings, false)
	}

	// Six ordinary weekly backups of 100 compressible documents, one edit each week.
	text := strings.Repeat("quarterly report, nothing unusual here. ", 300)
	for i := 0; i < 100; i++ {
		if err := os.WriteFile(filepath.Join(jobRoot, fmt.Sprintf("report-%03d.txt", i)), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for week := 0; week < 6; week++ {
		if err := os.WriteFile(filepath.Join(jobRoot, fmt.Sprintf("report-%03d.txt", week)), []byte(text+fmt.Sprint(week)), 0o644); err != nil {
			t.Fatal(err)
		}
		cycle()
		if st := runner.StateStore.Get(jobRoot); st.LastStatus != "success" {
			t.Fatalf("week %d: %s (%s)", week, st.LastStatus, st.LastErrorDetail)
		}
	}
	if uploads != 6 {
		t.Fatalf("uploads = %d, want 6", uploads)
	}

	// Ransomware: every document replaced by an encrypted copy with a new extension.
	noise := make([]byte, len(text)+64)
	for i := 0; i < 100; i++ {
		name := filepath.Join(jobRoot, fmt.Sprintf("report-%03d.txt", i))
		rand.Read(noise)
		if err := os.WriteFile(name+".locked", noise, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(name); err != nil {
			t.Fatal(err)
		}
	}
	recorder := &recordingStateStore{jobStateStore: runner.StateStore}
	runner.StateStore = recorder
	cycle()
	held := runner.StateStore.Get(jobRoot)
	if held.LastStatus != heldForReviewStatus || !held.ManualInterventionRequired || held.PendingArchive == "" {
		t.Fatalf("not held: %+v", held)
	}
	// A restart can only see saved state, so no save may mark the new archive
	// ready to upload before it was reviewed and held.
	for _, saved := range recorder.saved {
		if saved.PendingArchive == held.PendingArchive && !saved.ManualInterventionRequired {
			t.Fatalf("archive saved as ready to upload before its hold: status %q", saved.LastStatus)
		}
	}
	for _, want := range []string{"barely compresses", ".locked", "mass renaming"} {
		if !strings.Contains(held.LastErrorDetail, want) {
			t.Errorf("detail %q lacks %q", held.LastErrorDetail, want)
		}
	}
	if uploads != 6 {
		t.Fatalf("held archive was uploaded: %d uploads", uploads)
	}

	// After a restart, a fresh runner on the same saved state leaves it held,
	// with its reasons, and doesn't upload.
	restarted := testRunner(t, settings, client)
	restarted.StateStore, restarted.Anomalies = runner.StateStore, runner.Anomalies
	runner = restarted
	cycle()
	again := runner.StateStore.Get(jobRoot)
	if again.LastStatus != heldForReviewStatus || again.LastErrorDetail != held.LastErrorDetail || uploads != 6 {
		t.Fatalf("after restart: status %s, uploads %d, detail %q", again.LastStatus, uploads, again.LastErrorDetail)
	}

	// Force Upload sends the held archive and makes it part of the history.
	if _, err := runner.ForceSendJob(context.Background(), "docs"); err != nil {
		t.Fatal(err)
	}
	if st := runner.StateStore.Get(jobRoot); st.LastStatus != "success" || uploads != 7 {
		t.Fatalf("after force upload: %s, %d uploads", st.LastStatus, uploads)
	}
	history, err := runner.Anomalies.History(jobRoot)
	if err != nil || len(history) != 7 {
		t.Fatalf("history = %d, err %v", len(history), err)
	}
}

func TestAlertModeUploadsUnusualBackups(t *testing.T) {
	settings := testRunnerSettings(t)
	settings.AnomalyMode = anomaly.ModeAlert
	runner := testRunner(t, settings, testUploadClient("http://example.invalid"))

	jobRoot := filepath.Join(settings.ScanRoot, "docs")
	job := &backup.JobDefinition{RootPath: jobRoot, JobName: "docs"}
	history := anomaly.Observe([]*backup.DiscoveredFile{{ArchivePath: "a.txt", Size: 1}}, 1, timeZero)
	for i := 0; i < 6; i++ {
		history.FileCount = 1000
		if err := runner.Anomalies.SetPending(jobRoot, history); err != nil {
			t.Fatal(err)
		}
		if err := runner.Anomalies.Accept(jobRoot); err != nil {
			t.Fatal(err)
		}
	}
	files := []*backup.DiscoveredFile{{ArchivePath: "a.txt", Size: 1}}
	if review := runner.reviewStagedArchive(job, settings, files, 1, false); review == nil || review.hold {
		t.Fatalf("alert mode review = %+v, want an alert without a hold", review)
	}
	settings.AnomalyMode = anomaly.ModeHold
	if review := runner.reviewStagedArchive(job, settings, files, 1, false); review == nil || !review.hold {
		t.Fatalf("hold mode review = %+v, want a hold for a 99.9%% drop", review)
	}
	if review := runner.reviewStagedArchive(job, settings, files, 1, true); review != nil {
		t.Fatalf("Force Upload was reviewed: %+v", review)
	}
	settings.AnomalyMode = anomaly.ModeOff
	if review := runner.reviewStagedArchive(job, settings, files, 1, false); review != nil {
		t.Fatalf("off mode review = %+v", review)
	}
}

// recordingStateStore keeps a copy of every saved state.
type recordingStateStore struct {
	jobStateStore
	saved []state.JobState
}

func (r *recordingStateStore) Set(key string, s state.JobState) error {
	r.saved = append(r.saved, s)
	return r.jobStateStore.Set(key, s)
}
