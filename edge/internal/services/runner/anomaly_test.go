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
		os.WriteFile(filepath.Join(jobRoot, fmt.Sprintf("report-%03d.txt", i)), []byte(text), 0o644)
	}
	for week := 0; week < 6; week++ {
		os.WriteFile(filepath.Join(jobRoot, fmt.Sprintf("report-%03d.txt", week)), []byte(text+fmt.Sprint(week)), 0o644)
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
		os.WriteFile(name+".locked", noise, 0o644)
		os.Remove(name)
	}
	cycle()
	held := runner.StateStore.Get(jobRoot)
	if held.LastStatus != heldForReviewStatus || !held.ManualInterventionRequired || held.PendingArchive == "" {
		t.Fatalf("not held: %+v", held)
	}
	for _, want := range []string{"barely compresses", ".locked", "mass renaming"} {
		if !strings.Contains(held.LastErrorDetail, want) {
			t.Errorf("detail %q lacks %q", held.LastErrorDetail, want)
		}
	}
	if uploads != 6 {
		t.Fatalf("held archive was uploaded: %d uploads", uploads)
	}

	// The next cycle leaves it held, with its reasons, and doesn't upload.
	cycle()
	again := runner.StateStore.Get(jobRoot)
	if again.LastStatus != heldForReviewStatus || again.LastErrorDetail != held.LastErrorDetail || uploads != 6 {
		t.Fatalf("second cycle: status %s, uploads %d, detail %q", again.LastStatus, uploads, again.LastErrorDetail)
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
		runner.Anomalies.SetPending(jobRoot, history)
		runner.Anomalies.Accept(jobRoot)
	}
	files := []*backup.DiscoveredFile{{ArchivePath: "a.txt", Size: 1}}
	s := runner.StateStore.Get(jobRoot)
	if runner.holdUnusualBackup(job, &s, settings, files, 1, false) {
		t.Fatal("alert mode held the backup")
	}
	settings.AnomalyMode = anomaly.ModeHold
	if !runner.holdUnusualBackup(job, &s, settings, files, 1, false) {
		t.Fatal("hold mode let a 99.9% drop through")
	}
	settings.AnomalyMode = anomaly.ModeOff
	if runner.holdUnusualBackup(job, &s, settings, files, 1, false) {
		t.Fatal("off mode held the backup")
	}
}
