package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/3to1go/central/internal/storage"
)

func newTestBackend(t *testing.T) (*storage.LocalBackend, string) {
	t.Helper()
	root := t.TempDir()
	return storage.NewLocalBackend(root), root
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}

func setMtime(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func TestPruneOldSnapshots_NothingToDelete(t *testing.T) {
	backend, root := newTestBackend(t)
	ns := "edge/inst/job"
	nsDir := filepath.Join(root, ns)
	os.MkdirAll(nsDir, 0o755)
	writeFile(t, filepath.Join(nsDir, "a.tar.zst"), "data")
	writeFile(t, filepath.Join(nsDir, "b.tar.zst"), "data")

	deleted, err := PruneOldSnapshots(backend, ns, 3)
	if err != nil {
		t.Fatalf("PruneOldSnapshots: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
}

func TestPruneOldSnapshots_DeletesOldest(t *testing.T) {
	backend, root := newTestBackend(t)
	ns := "edge/inst/job"
	nsDir := filepath.Join(root, ns)
	os.MkdirAll(nsDir, 0o755)

	now := time.Now()
	files := []struct {
		name  string
		mtime time.Time
	}{
		{"oldest.tar.zst", now.Add(-3 * time.Hour)},
		{"middle.tar.zst", now.Add(-2 * time.Hour)},
		{"newest.tar.zst", now.Add(-1 * time.Hour)},
	}
	for _, f := range files {
		writeFile(t, filepath.Join(nsDir, f.name), "data")
		setMtime(t, filepath.Join(nsDir, f.name), f.mtime)
	}

	deleted, err := PruneOldSnapshots(backend, ns, 2)
	if err != nil {
		t.Fatalf("PruneOldSnapshots: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	// oldest should be gone
	if _, err := os.Stat(filepath.Join(nsDir, "oldest.tar.zst")); !os.IsNotExist(err) {
		t.Error("oldest.tar.zst should have been deleted")
	}
	// newest should remain
	if _, err := os.Stat(filepath.Join(nsDir, "newest.tar.zst")); err != nil {
		t.Errorf("newest.tar.zst should remain: %v", err)
	}
}

func TestPruneOldSnapshots_DeleteAll(t *testing.T) {
	backend, root := newTestBackend(t)
	ns := "edge/inst/job"
	nsDir := filepath.Join(root, ns)
	os.MkdirAll(nsDir, 0o755)

	now := time.Now()
	for i := 0; i < 5; i++ {
		name := "file" + string(rune('a'+i)) + ".tar.zst"
		writeFile(t, filepath.Join(nsDir, name), "data")
		setMtime(t, filepath.Join(nsDir, name), now.Add(time.Duration(i)*time.Minute))
	}

	deleted, err := PruneOldSnapshots(backend, ns, 1)
	if err != nil {
		t.Fatalf("PruneOldSnapshots: %v", err)
	}
	if deleted != 4 {
		t.Errorf("deleted = %d, want 4", deleted)
	}
}

func TestPruneOldSnapshots_EmptyNamespace(t *testing.T) {
	backend, _ := newTestBackend(t)
	deleted, err := PruneOldSnapshots(backend, "no/such/ns", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
}

func TestPruneOldSnapshots_SameMtimeSortsByName(t *testing.T) {
	backend, root := newTestBackend(t)
	ns := "edge/inst/job"
	nsDir := filepath.Join(root, ns)
	os.MkdirAll(nsDir, 0o755)

	mtime := time.Now()
	for _, name := range []string{"aaa.tar.zst", "bbb.tar.zst", "ccc.tar.zst"} {
		writeFile(t, filepath.Join(nsDir, name), "data")
		setMtime(t, filepath.Join(nsDir, name), mtime)
	}

	deleted, err := PruneOldSnapshots(backend, ns, 2)
	if err != nil {
		t.Fatalf("PruneOldSnapshots: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	// "aaa" should be deleted (lowest by name = oldest when mtimes equal)
	if _, err := os.Stat(filepath.Join(nsDir, "aaa.tar.zst")); !os.IsNotExist(err) {
		t.Error("aaa.tar.zst should have been pruned")
	}
}
