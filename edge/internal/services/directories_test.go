package services

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/3to1go/edge/internal/backup"
	"github.com/3to1go/edge/internal/config"
)

// ---------------------------------------------------------------------------
// Mock jobStateStore
// ---------------------------------------------------------------------------

type mockStateStore struct {
	mu     sync.Mutex
	states map[string]JobState
	setErr error
	delErr error
}

func newMockStateStore() *mockStateStore {
	return &mockStateStore{states: make(map[string]JobState)}
}

func (m *mockStateStore) Get(key string) JobState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.states[key]
}

func (m *mockStateStore) Set(key string, state JobState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.states == nil {
		m.states = make(map[string]JobState)
	}
	m.states[key] = state
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
	return svc, ms
}

func writeMarker(t *testing.T, dir string, payload map[string]any) {
	t.Helper()
	if err := backup.WriteUploadDir(dir, payload); err != nil {
		t.Fatalf("WriteUploadDir: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ListDirectories
// ---------------------------------------------------------------------------

func TestListDirectories_EmptyScanRoot(t *testing.T) {
	root := t.TempDir()
	svc, _ := newDirService(t, root)
	entries, err := svc.ListDirectories()
	if err != nil {
		t.Fatalf("ListDirectories: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry (root itself), got %d", len(entries))
	}
	if entries[0].Selected {
		t.Error("root should not be selected (no marker)")
	}
}

func TestListDirectories_WithSubdirectory(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "photos"), fs.ModePerm)
	os.Mkdir(filepath.Join(root, "docs"), fs.ModePerm)

	svc, _ := newDirService(t, root)
	entries, err := svc.ListDirectories()
	if err != nil {
		t.Fatalf("ListDirectories: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries (root + 2 subdirs), got %d", len(entries))
	}
}

func TestListDirectories_WithMarkerFile(t *testing.T) {
	root := t.TempDir()
	photoDir := filepath.Join(root, "photos")
	os.Mkdir(photoDir, fs.ModePerm)
	writeMarker(t, photoDir, map[string]any{"job_name": "photos"})

	svc, _ := newDirService(t, root)
	entries, err := svc.ListDirectories()
	if err != nil {
		t.Fatalf("ListDirectories: %v", err)
	}

	var photosEntry *DirectoryEntry
	for i := range entries {
		if entries[i].RelativePath == "photos" {
			photosEntry = &entries[i]
			break
		}
	}
	if photosEntry == nil {
		t.Fatal("expected 'photos' entry in results")
	}
	if !photosEntry.Selected {
		t.Error("photos directory should be selected (has marker)")
	}
	if photosEntry.Config == nil {
		t.Error("photos directory should have Config set")
	}
}

func TestListDirectories_ChildBlockedByParentWithMarker(t *testing.T) {
	root := t.TempDir()
	parentDir := filepath.Join(root, "parent")
	childDir := filepath.Join(parentDir, "child")
	os.MkdirAll(childDir, fs.ModePerm)
	writeMarker(t, parentDir, map[string]any{"job_name": "parent"})

	svc, _ := newDirService(t, root)
	entries, err := svc.ListDirectories()
	if err != nil {
		t.Fatalf("ListDirectories: %v", err)
	}

	var childEntry *DirectoryEntry
	for i := range entries {
		if entries[i].RelativePath == "parent/child" {
			childEntry = &entries[i]
			break
		}
	}
	if childEntry == nil {
		t.Fatal("expected parent/child entry")
	}
	if childEntry.BlockedByParent == nil {
		t.Error("child should be blocked by parent")
	}
}

// ---------------------------------------------------------------------------
// SaveJob
// ---------------------------------------------------------------------------

func TestSaveJob_CreatesMarkerFile(t *testing.T) {
	root := t.TempDir()
	photoDir := filepath.Join(root, "photos")
	os.Mkdir(photoDir, fs.ModePerm)

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

func TestSaveJob_NestedUnderExistingJob(t *testing.T) {
	root := t.TempDir()
	parentDir := filepath.Join(root, "parent")
	childDir := filepath.Join(parentDir, "child")
	os.MkdirAll(childDir, fs.ModePerm)
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
	os.Mkdir(photoDir, fs.ModePerm)
	writeMarker(t, photoDir, map[string]any{"job_name": "photos"})

	svc, ms := newDirService(t, root)
	// Pre-seed some state.
	ms.Set(photoDir, JobState{LastStatus: "success"})

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
	os.Mkdir(photoDir, fs.ModePerm)
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
	os.Mkdir(filepath.Join(root, "photos"), fs.ModePerm)
	svc, _ := newDirService(t, root)
	_, err := svc.LoadJob("photos")
	if err == nil {
		t.Error("expected error when no marker file")
	}
}
