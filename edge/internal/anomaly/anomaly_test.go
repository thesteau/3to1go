package anomaly

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/3to1go/edge/internal/backup"
	"github.com/3to1go/edge/internal/testutil"
	"github.com/DATA-DOG/go-sqlmock"
)

// folder builds a file list of n documents, with sizes that vary per version.
func folder(n, version int, ext string) []*backup.DiscoveredFile {
	files := make([]*backup.DiscoveredFile, n)
	for i := range files {
		files[i] = &backup.DiscoveredFile{
			ArchivePath: fmt.Sprintf("docs/report-%03d%s", i, ext),
			Size:        int64(40_000 + i*100 + version),
		}
	}
	return files
}

func totalSize(files []*backup.DiscoveredFile) int64 {
	var total int64
	for _, f := range files {
		total += f.Size
	}
	return total
}

// observe makes an observation whose archive compresses to ratio of the source.
func observe(files []*backup.DiscoveredFile, ratio float64) Observation {
	return Observe(files, int64(float64(totalSize(files))*ratio), time.Unix(0, 0))
}

// normalHistory is ten weekly backups of a 200-file folder that compresses to
// about 40%, where a few files change each week.
func normalHistory() []Observation {
	history := make([]Observation, 10)
	for week := range history {
		files := folder(200, 0, ".docx")
		for i := 0; i < 5; i++ {
			files[(week*5+i)%200].Size += int64(week + 1)
		}
		history[week] = observe(files, 0.40+float64(week%3)/100)
	}
	return history
}

func signals(r Result) []string {
	out := make([]string, len(r.Findings))
	for i, f := range r.Findings {
		out[i] = f.Signal
	}
	slices.Sort(out)
	return out
}

func TestOrdinaryChangesAreNotFlagged(t *testing.T) {
	files := folder(210, 0, ".docx") // ten new files, a few edits
	files[3].Size += 500
	if r := Evaluate(normalHistory(), observe(files, 0.41)); r.Unusual() {
		t.Errorf("flagged an ordinary week: %s", r.Summary())
	}
}

func TestTruncatedFilesInASmallJobAreFlagged(t *testing.T) {
	// Twelve files, too few for the churn and file-type checks, all wiped to 0 bytes.
	var history []Observation
	for week := 0; week < 6; week++ {
		history = append(history, observe(folder(12, week, ".txt"), 0.40))
	}
	wiped := folder(12, 0, ".txt")
	for _, f := range wiped {
		f.Size = 0
	}
	r := Evaluate(history, observe(wiped, 0))
	if got := signals(r); !slices.Equal(got, []string{"size"}) {
		t.Fatalf("signals = %v (%s)", got, r.Summary())
	}
	if !strings.Contains(r.Summary(), "The folder holds 0 B, but usually holds about") {
		t.Errorf("summary = %q", r.Summary())
	}
}

func TestEmptiedFolderIsFlagged(t *testing.T) {
	r := Evaluate(normalHistory(), observe(folder(30, 0, ".docx"), 0.40))
	if got := signals(r); !slices.Equal(got, []string{"file_count", "size"}) {
		t.Fatalf("signals = %v (%s)", got, r.Summary())
	}
	if !strings.Contains(r.Summary(), "The folder has 30 files, but usually has about 200.") {
		t.Errorf("summary = %q", r.Summary())
	}
}

func TestRansomwarePatternIsFlagged(t *testing.T) {
	// Every file renamed to .locked, slightly larger, and no longer compressible.
	encrypted := folder(200, 16, ".docx.locked")
	r := Evaluate(normalHistory(), observe(encrypted, 1.0))
	if got := signals(r); !slices.Equal(got, []string{"churn", "compression", "file_types"}) {
		t.Fatalf("signals = %v (%s)", got, r.Summary())
	}
	for _, want := range []string{"barely compresses", "Files ending in .locked went from 0% to 100%", "mass renaming"} {
		if !strings.Contains(r.Summary(), want) {
			t.Errorf("summary %q lacks %q", r.Summary(), want)
		}
	}
}

func TestBulkImportIsNotTreatedAsRewriting(t *testing.T) {
	// 300 new photos added: the count jumps, but nothing was renamed or encrypted.
	files := folder(200, 0, ".docx")
	for i := 0; i < 300; i++ {
		files = append(files, &backup.DiscoveredFile{ArchivePath: fmt.Sprintf("photos/img-%03d.jpg", i), Size: 2_000})
	}
	got := signals(Evaluate(normalHistory(), observe(files, 0.40)))
	if !slices.Equal(got, []string{"file_count"}) {
		t.Errorf("signals = %v, want only file_count", got)
	}
}

func TestPhotoFoldersThatNeverCompressAreNotFlagged(t *testing.T) {
	var history []Observation
	for week := 0; week < 6; week++ {
		history = append(history, observe(folder(200, week, ".jpg"), 0.99))
	}
	if r := Evaluate(history, observe(folder(205, 9, ".jpg"), 1.0)); slices.Contains(signals(r), "compression") {
		t.Errorf("flagged incompressible photos: %s", r.Summary())
	}
}

func TestShortHistorySkipsSizeChecks(t *testing.T) {
	history := normalHistory()[:2]
	r := Evaluate(history, observe(folder(30, 0, ".docx"), 0.40))
	if r.Unusual() {
		t.Errorf("size checks ran without enough history: %s", r.Summary())
	}
}

func TestSimilarityEstimatesSharedFiles(t *testing.T) {
	a := observe(folder(400, 0, ".txt"), 0.5)
	half := folder(400, 0, ".txt")
	for i := 0; i < 200; i++ {
		half[i].Size++
	}
	// 200 shared of 600 distinct path-size pairs: a Jaccard similarity of 1/3.
	if got := Similarity(a, observe(half, 0.5)); got < 0.2 || got > 0.47 {
		t.Errorf("similarity = %.2f, want about 0.33", got)
	}
	if got := Similarity(a, a); got != 1 {
		t.Errorf("self similarity = %v", got)
	}
}

func TestStoreAcceptsOnlyUploadedObservations(t *testing.T) {
	db, mock := testutil.MockDB(t)
	s := NewStore(db)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS job_anomaly").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}

	obs := observe(folder(20, 0, ".txt"), 0.5)
	load := func(history []Observation, pending *Observation) {
		t.Helper()
		raw, err := json.Marshal(history)
		if err != nil {
			t.Fatal(err)
		}
		pendingRaw := ""
		if pending != nil {
			encoded, err := json.Marshal(pending)
			if err != nil {
				t.Fatal(err)
			}
			pendingRaw = string(encoded)
		}
		mock.ExpectQuery("SELECT history, pending FROM job_anomaly WHERE key =").WithArgs("job").WillReturnRows(sqlmock.NewRows([]string{"history", "pending"}).AddRow(string(raw), pendingRaw))
	}
	stage := func(obs Observation) {
		t.Helper()
		raw, err := json.Marshal(obs)
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectExec("INSERT INTO job_anomaly").WithArgs("job", string(raw)).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectQuery("SELECT history, pending FROM job_anomaly WHERE key =").WithArgs("job").WillReturnRows(sqlmock.NewRows([]string{"history", "pending"}))
	if err := s.Accept("job"); err != nil {
		t.Fatalf("accept without pending: %v", err)
	}
	stage(obs)
	if err := s.SetPending("job", obs); err != nil {
		t.Fatal(err)
	}
	load(nil, &obs)
	if h, _ := s.History("job"); len(h) != 0 {
		t.Fatalf("pending leaked into history: %d", len(h))
	}
	mock.ExpectExec("UPDATE job_anomaly SET pending = '' WHERE key =").WithArgs("job").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.ClearPending("job"); err != nil {
		t.Fatal(err)
	}
	load(nil, nil)
	if err := s.Accept("job"); err != nil {
		t.Fatal(err)
	}
	load(nil, nil)
	if h, _ := s.History("job"); len(h) != 0 {
		t.Fatalf("cleared pending was accepted: %d", len(h))
	}

	var history []Observation
	for i := 0; i < 25; i++ {
		obs.FileCount = i
		stage(obs)
		if err := s.SetPending("job", obs); err != nil {
			t.Fatal(err)
		}
		load(history, &obs)
		history = append(history, obs)
		if len(history) > 20 {
			history = history[1:]
		}
		raw, err := json.Marshal(history)
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectExec("UPDATE job_anomaly SET history =").WithArgs(string(raw), "job").WillReturnResult(sqlmock.NewResult(0, 1))
		if err := s.Accept("job"); err != nil {
			t.Fatal(err)
		}
	}
	load(history, nil)
	h, err := s.History("job")
	if err != nil || len(h) != 20 || h[0].FileCount != 5 || h[19].FileCount != 24 {
		t.Fatalf("history = %d entries, first %d, err %v", len(h), h[0].FileCount, err)
	}
	mock.ExpectExec("DELETE FROM job_anomaly WHERE key =").WithArgs("job").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.Delete("job"); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT history, pending FROM job_anomaly WHERE key =").WithArgs("job").WillReturnRows(sqlmock.NewRows([]string{"history", "pending"}))
	if h, _ := s.History("job"); h != nil {
		t.Errorf("history after delete = %v", h)
	}
}
