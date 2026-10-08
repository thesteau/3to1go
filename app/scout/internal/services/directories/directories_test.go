package directories

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/3to1go/scout/internal/backup"
	"github.com/3to1go/scout/internal/config"
	"github.com/3to1go/scout/internal/services/state"
)

// ---------------------------------------------------------------------------
// Mock jobStateStore
// ---------------------------------------------------------------------------

type mockStateStore struct {
	mu     sync.Mutex
	states map[string]state.JobState
	setErr error
	delErr error
}

func newMockStateStore() *mockStateStore {
	return &mockStateStore{states: make(map[string]state.JobState)}
}

func (m *mockStateStore) Get(key string) state.JobState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.states[key]
}

func (m *mockStateStore) Set(key string, s state.JobState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.states == nil {
		m.states = make(map[string]state.JobState)
	}
	m.states[key] = s
	return m.setErr
}

func (m *mockStateStore) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, key)
	return m.delErr
}

func (m *mockStateStore) wasDeleted(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.states[key]
	return !ok
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func discardSlogLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 100}))
}

func newDirService(t *testing.T, scanRoot string) (*DirectoryService, *mockStateStore) {
	t.Helper()
	ms := newMockStateStore()
	settings := &config.Settings{ScanRoot: scanRoot, MaxDepth: 5}
	svc := NewDirectoryService(settings, discardSlogLogger(), ms)
	// Saves and deletes start a background walk; let it end before the folder is removed.
	t.Cleanup(func() { waitForWalks(t, svc) })
	return svc, ms
}

func writeMarker(t *testing.T, dir string, payload map[string]any) {
	t.Helper()
	if err := backup.WriteUploadDir(dir, payload); err != nil {
		t.Fatalf("WriteUploadDir: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ListJobs
// ---------------------------------------------------------------------------

func jobPaths(t *testing.T, svc *DirectoryService) []string {
	t.Helper()
	entries, err := svc.ListJobs()
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	paths := []string{}
	for _, entry := range entries {
		if !entry.Selected {
			t.Errorf("%s listed without a marker", entry.RelativePath)
		}
		paths = append(paths, entry.RelativePath)
	}
	return paths
}

func TestListJobs_FindsMarkersWithinDepthWithoutEnteringJobs(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"photos/nested", "docs/work/q1", "deep/a/b/c/d/e", "plain"} {
		if err := os.MkdirAll(filepath.Join(root, dir), fs.ModePerm); err != nil {
			t.Fatal(err)
		}
	}
	writeMarker(t, filepath.Join(root, "photos"), map[string]any{"job_name": "photos"})
	writeMarker(t, filepath.Join(root, "photos", "nested"), map[string]any{"job_name": "nested"})
	writeMarker(t, filepath.Join(root, "docs", "work"), map[string]any{"job_name": "work"})
	writeMarker(t, filepath.Join(root, "deep", "a", "b", "c", "d", "e"), map[string]any{"job_name": "too_deep"})

	svc, _ := newDirService(t, root)
	got := jobPaths(t, svc)
	want := []string{"docs/work", "photos"}
	if !slices.Equal(got, want) {
		t.Errorf("jobs = %v, want %v", got, want)
	}
}

func TestListJobs_ReportsInvalidConfig(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "broken")
	if err := os.Mkdir(dir, fs.ModePerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, backup.UploadDirFilename), []byte("job_name: [unclosed"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc, _ := newDirService(t, root)
	entries, err := svc.ListJobs()
	if err != nil || len(entries) != 1 {
		t.Fatalf("ListJobs = %v, %v", entries, err)
	}
	if entries[0].ConfigError == nil {
		t.Error("invalid marker should report a config error")
	}
}

// controlledWalks replaces the folder walk with one the test releases by hand.
type controlledWalks struct {
	calls   chan struct{}
	release chan []string
}

func controlWalks(svc *DirectoryService) *controlledWalks {
	c := &controlledWalks{calls: make(chan struct{}, 10), release: make(chan []string)}
	svc.discoverFn = func() []string {
		c.calls <- struct{}{}
		return <-c.release
	}
	return c
}

func (c *controlledWalks) finish(t *testing.T, dirs ...string) {
	t.Helper()
	select {
	case <-c.calls:
	case <-time.After(5 * time.Second):
		t.Fatal("no walk started")
	}
	c.release <- dirs
}

// finishLater ends the next walk from another goroutine, for calls that wait on it.
func (c *controlledWalks) finishLater(dirs ...string) {
	go func() {
		<-c.calls
		c.release <- dirs
	}()
}

func waitForWalks(t *testing.T, svc *DirectoryService) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		svc.jobsMu.Lock()
		walking := svc.walking
		svc.jobsMu.Unlock()
		if !walking {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("walk did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestListJobs_DoesNotWaitForASlowFirstWalk(t *testing.T) {
	old := firstDiscoveryWait
	firstDiscoveryWait = 10 * time.Millisecond
	t.Cleanup(func() { firstDiscoveryWait = old })
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), fs.ModePerm); err != nil {
		t.Fatal(err)
	}
	writeMarker(t, filepath.Join(root, "docs"), map[string]any{"job_name": "docs"})
	svc, _ := newDirService(t, root)
	walks := controlWalks(svc)

	entries, discovering, _ := svc.ListJobsWithState()
	if len(entries) != 0 || !discovering {
		t.Fatalf("jobs = %v, discovering = %v; want an empty list while the first walk runs", entries, discovering)
	}
	walks.finish(t, filepath.Join(root, "docs"))
	waitForWalks(t, svc)
	entries, discovering, _ = svc.ListJobsWithState()
	if len(entries) != 1 || entries[0].RelativePath != "docs" || discovering {
		t.Errorf("after the walk: jobs = %v, discovering = %v", entries, discovering)
	}
}

func TestListJobs_ServesTheLastWalkWhileAnotherRuns(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"docs", "photos"} {
		if err := os.Mkdir(filepath.Join(root, dir), fs.ModePerm); err != nil {
			t.Fatal(err)
		}
		writeMarker(t, filepath.Join(root, dir), map[string]any{"job_name": dir})
	}
	svc, _ := newDirService(t, root)
	walks := controlWalks(svc)
	walks.finishLater(filepath.Join(root, "docs"))
	if got := jobPaths(t, svc); !slices.Equal(got, []string{"docs"}) {
		t.Fatalf("jobs = %v", got)
	}
	waitForWalks(t, svc)

	// An old list is still returned at once, and a new walk starts behind it.
	svc.jobsMu.Lock()
	svc.jobDirsAt = time.Now().Add(-2 * jobDiscoveryTTL)
	svc.jobsMu.Unlock()
	if got := jobPaths(t, svc); !slices.Equal(got, []string{"docs"}) {
		t.Errorf("stale jobs = %v", got)
	}
	walks.finish(t, filepath.Join(root, "docs"), filepath.Join(root, "photos"))
	waitForWalks(t, svc)
	if got := jobPaths(t, svc); !slices.Equal(got, []string{"docs", "photos"}) {
		t.Errorf("after the new walk: %v", got)
	}
}

func TestListJobs_SaveAndDeleteShowAtOnce(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"docs", "photos", "zeta"} {
		if err := os.Mkdir(filepath.Join(root, dir), fs.ModePerm); err != nil {
			t.Fatal(err)
		}
	}
	writeMarker(t, filepath.Join(root, "zeta"), map[string]any{"job_name": "zeta"})
	svc, ms := newDirService(t, root)
	walks := controlWalks(svc)
	walks.finishLater(filepath.Join(root, "zeta"))
	jobPaths(t, svc)
	waitForWalks(t, svc)

	if _, err := svc.SaveJob("photos", map[string]any{"job_name": "photos"}); err != nil {
		t.Fatalf("SaveJob: %v", err)
	}
	if err := ms.Set(filepath.Join(root, "photos"), state.JobState{LastStatus: "uploading"}); err != nil {
		t.Fatal(err)
	}
	entries, _ := svc.ListJobs()
	if len(entries) != 2 || entries[0].RelativePath != "photos" || entries[0].State.LastStatus != "uploading" {
		t.Errorf("after save, before the walk: %+v", entries)
	}
	walks.finish(t, filepath.Join(root, "photos"), filepath.Join(root, "zeta"))
	waitForWalks(t, svc)

	if err := svc.DeleteJob("photos"); err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}
	if got := jobPaths(t, svc); !slices.Equal(got, []string{"zeta"}) {
		t.Errorf("after delete: %v", got)
	}
	walks.finish(t, filepath.Join(root, "zeta"))
	waitForWalks(t, svc)
}

func TestListJobs_ChangeDuringAWalkIsKeptAndWalkedAgain(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "photos"), fs.ModePerm); err != nil {
		t.Fatal(err)
	}
	svc, _ := newDirService(t, root)
	walks := controlWalks(svc)
	svc.StartDiscovery()

	// Saved while the first walk runs; that walk already passed the folder.
	if _, err := svc.SaveJob("photos", map[string]any{"job_name": "photos"}); err != nil {
		t.Fatalf("SaveJob: %v", err)
	}
	walks.finish(t)
	// The change triggers a second walk, but the first result is published now.
	select {
	case <-svc.firstWalk:
	case <-time.After(5 * time.Second):
		t.Fatal("first walk not published")
	}
	if got := jobPaths(t, svc); !slices.Equal(got, []string{"photos"}) {
		t.Errorf("jobs = %v, want the saved job kept", got)
	}
	walks.finish(t, filepath.Join(root, "photos"))
	waitForWalks(t, svc)
}

func TestComparePaths_MatchesWalkOrder(t *testing.T) {
	paths := []string{"/r/b", "/r/A/z", "/r/a", "/r/B/c"}
	slices.SortFunc(paths, comparePaths)
	want := []string{"/r/a", "/r/A/z", "/r/b", "/r/B/c"}
	if !slices.Equal(paths, want) {
		t.Errorf("order = %v, want %v", paths, want)
	}
}

// ---------------------------------------------------------------------------
// ListChildren
// ---------------------------------------------------------------------------

func childrenByPath(t *testing.T, svc *DirectoryService, relativePath string) map[string]DirectoryNode {
	t.Helper()
	nodes, err := svc.ListChildren(relativePath)
	if err != nil {
		t.Fatalf("ListChildren(%q): %v", relativePath, err)
	}
	result := map[string]DirectoryNode{}
	for _, node := range nodes {
		result[node.RelativePath] = node
	}
	return result
}

func TestListChildren_ListsOneLevelWithCounts(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"photos/2024", "photos/.thumbs", "docs"} {
		if err := os.MkdirAll(filepath.Join(root, dir), fs.ModePerm); err != nil {
			t.Fatal(err)
		}
	}
	writeMarker(t, filepath.Join(root, "photos"), map[string]any{"job_name": "photos"})

	svc, _ := newDirService(t, root)
	nodes, err := svc.ListChildren(".")
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	if len(nodes) != 2 || nodes[0].RelativePath != "docs" || nodes[1].RelativePath != "photos" {
		t.Fatalf("nodes = %+v", nodes)
	}
	photos := nodes[1]
	if !photos.Selected || photos.Config == nil || photos.BlockedByParent != nil {
		t.Errorf("photos = %+v", photos)
	}
	if photos.ChildCount != 2 || photos.HiddenChildCount != 1 {
		t.Errorf("photos counts = %d/%d, want 2/1", photos.ChildCount, photos.HiddenChildCount)
	}
	if nodes[0].ChildCount != 0 {
		t.Errorf("docs child count = %d, want 0", nodes[0].ChildCount)
	}
}

func TestListChildren_ReportsParentJobBlocksAndExclusions(t *testing.T) {
	root := t.TempDir()
	parentDir := filepath.Join(root, "parent")
	if err := os.MkdirAll(filepath.Join(parentDir, "skip", "deeper"), fs.ModePerm); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parentDir, "keep"), fs.ModePerm); err != nil {
		t.Fatal(err)
	}
	writeMarker(t, parentDir, map[string]any{"job_name": "parent"})

	svc, _ := newDirService(t, root)
	if err := svc.ExcludePath("parent/skip"); err != nil {
		t.Fatalf("ExcludePath: %v", err)
	}
	children := childrenByPath(t, svc, "parent")
	if children["parent/keep"].Excluded || !children["parent/skip"].Excluded {
		t.Errorf("exclusions = keep:%v skip:%v", children["parent/keep"].Excluded, children["parent/skip"].Excluded)
	}
	if children["parent/keep"].BlockedByParent != "parent" {
		t.Errorf("blocked_by_parent = %v, want parent", children["parent/keep"].BlockedByParent)
	}
	deeper := childrenByPath(t, svc, "parent/skip")["parent/skip/deeper"]
	if !deeper.Excluded || deeper.BlockedByParent != "parent" {
		t.Errorf("deeper = %+v", deeper)
	}
}

func TestListChildren_StopsAtMaxDepthAndRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b", "c", "d", "e", "f"), fs.ModePerm); err != nil {
		t.Fatal(err)
	}
	svc, _ := newDirService(t, root) // MaxDepth 5

	d := childrenByPath(t, svc, "a/b/c")["a/b/c/d"]
	if d.ChildCount != 1 {
		t.Errorf("depth-4 child count = %d, want 1", d.ChildCount)
	}
	e := childrenByPath(t, svc, "a/b/c/d")["a/b/c/d/e"]
	if e.ChildCount != 0 {
		t.Errorf("max-depth folder child count = %d, want 0", e.ChildCount)
	}
	if got := childrenByPath(t, svc, "a/b/c/d/e"); len(got) != 0 {
		t.Errorf("children beyond max depth = %v", got)
	}
	if _, err := svc.ListChildren("../escape"); err == nil {
		t.Error("expected traversal to be rejected")
	}
}

func TestListChildren_RejectsSymlinkLeavingScanRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "secret"), fs.ModePerm); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "inside"), fs.ModePerm); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "inside"), filepath.Join(root, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	svc, _ := newDirService(t, root)
	if nodes, err := svc.ListChildren("escape"); err == nil {
		t.Errorf("expected symlink outside the scan root to be rejected, got %+v", nodes)
	}
	if _, err := svc.ListChildren("alias"); err != nil {
		t.Errorf("symlink within the scan root: %v", err)
	}
	if _, err := svc.ListChildren("inside/missing"); err == nil {
		t.Error("expected a missing folder to be rejected")
	}
}

// ---------------------------------------------------------------------------
// SaveJob
// ---------------------------------------------------------------------------

func TestSaveJob_CreatesMarkerFile(t *testing.T) {
	root := t.TempDir()
	photoDir := filepath.Join(root, "photos")
	if err := os.Mkdir(photoDir, fs.ModePerm); err != nil {
		t.Fatal(err)
	}

	svc, _ := newDirService(t, root)
	entry, err := svc.SaveJob("photos", map[string]any{"job_name": "myphotos"})
	if err != nil {
		t.Fatalf("SaveJob: %v", err)
	}
	if !entry.Selected {
		t.Error("saved job should be selected")
	}
	// Marker file must exist.
	markerPath := filepath.Join(photoDir, backup.UploadDirFilename)
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Error("marker file should exist after SaveJob")
	}
}

func TestSaveJob_DirectoryNotFound(t *testing.T) {
	root := t.TempDir()
	svc, _ := newDirService(t, root)
	_, err := svc.SaveJob("nonexistent", nil)
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestSaveJob_PathTraversalRejected(t *testing.T) {
	root := t.TempDir()
	svc, _ := newDirService(t, root)
	_, err := svc.SaveJob("../escape", nil)
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestJobOperations_RejectSymlinksLeavingScanRoot(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	writeMarker(t, outside, map[string]any{"job_name": "outside"})
	marker := filepath.Join(outside, backup.UploadDirFilename)
	before, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	svc, _ := newDirService(t, root)
	for _, path := range []string{"escape", "escape/child/.."} {
		if _, err := svc.SaveJob(path, map[string]any{"job_name": "changed"}); err == nil {
			t.Errorf("saved job outside scan root through %q", path)
		}
		if _, err := svc.LoadJob(path); err == nil {
			t.Errorf("loaded job outside scan root through %q", path)
		}
		if err := svc.DeleteJob(path); err == nil {
			t.Errorf("deleted job outside scan root through %q", path)
		}
	}
	after, err := os.ReadFile(marker)
	if err != nil || string(after) != string(before) {
		t.Fatalf("outside marker changed: %q, %v", after, err)
	}
}

func TestJobOperations_AllowSymlinksWithinScanRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "inside")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(inside, filepath.Join(root, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	svc, _ := newDirService(t, root)
	if _, err := svc.SaveJob("alias", map[string]any{"job_name": "inside"}); err != nil {
		t.Fatal(err)
	}
	if job, err := svc.LoadJob("alias"); err != nil || job.JobName != "inside" {
		t.Fatalf("load through alias: %v, %v", job, err)
	}
	if err := svc.DeleteJob("alias"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(inside, backup.UploadDirFilename)); !os.IsNotExist(err) {
		t.Fatalf("marker remains after deletion: %v", err)
	}
}

func TestSaveJob_NestedUnderExistingJob(t *testing.T) {
	root := t.TempDir()
	parentDir := filepath.Join(root, "parent")
	childDir := filepath.Join(parentDir, "child")
	if err := os.MkdirAll(childDir, fs.ModePerm); err != nil {
		t.Fatal(err)
	}
	writeMarker(t, parentDir, map[string]any{})

	svc, _ := newDirService(t, root)
	_, err := svc.SaveJob("parent/child", nil)
	if err == nil {
		t.Error("expected error when saving job nested under existing job")
	}
}

// ---------------------------------------------------------------------------
// DeleteJob
// ---------------------------------------------------------------------------

func TestDeleteJob_RemovesMarkerAndClearsState(t *testing.T) {
	root := t.TempDir()
	photoDir := filepath.Join(root, "photos")
	if err := os.Mkdir(photoDir, fs.ModePerm); err != nil {
		t.Fatal(err)
	}
	writeMarker(t, photoDir, map[string]any{"job_name": "photos"})

	svc, ms := newDirService(t, root)
	// Pre-seed some state.
	if err := ms.Set(photoDir, state.JobState{LastStatus: "success"}); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteJob("photos"); err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}

	markerPath := filepath.Join(photoDir, backup.UploadDirFilename)
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("marker file should not exist after DeleteJob")
	}
	if !ms.wasDeleted(photoDir) {
		t.Error("state should have been deleted")
	}
}

func TestDeleteJob_NonexistentDirectory(t *testing.T) {
	root := t.TempDir()
	svc, _ := newDirService(t, root)
	err := svc.DeleteJob("ghost")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

// ---------------------------------------------------------------------------
// LoadJob
// ---------------------------------------------------------------------------

func TestLoadJob_Success(t *testing.T) {
	root := t.TempDir()
	photoDir := filepath.Join(root, "photos")
	if err := os.Mkdir(photoDir, fs.ModePerm); err != nil {
		t.Fatal(err)
	}
	writeMarker(t, photoDir, map[string]any{"job_name": "myphotos"})

	svc, _ := newDirService(t, root)
	job, err := svc.LoadJob("photos")
	if err != nil {
		t.Fatalf("LoadJob: %v", err)
	}
	if job.JobName != "myphotos" {
		t.Errorf("JobName = %q, want myphotos", job.JobName)
	}
}

func TestLoadJob_NoMarker(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "photos"), fs.ModePerm); err != nil {
		t.Fatal(err)
	}
	svc, _ := newDirService(t, root)
	_, err := svc.LoadJob("photos")
	if err == nil {
		t.Error("expected error when no marker file")
	}
}
