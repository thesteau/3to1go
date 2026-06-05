package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func newBackend(t *testing.T) (*LocalBackend, string) {
	t.Helper()
	root := t.TempDir()
	return NewLocalBackend(root), root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}

// --- NewLocalBackend ---

func TestNewLocalBackend_ProbePath(t *testing.T) {
	b, root := newBackend(t)
	if b.BackupRoot != root {
		t.Errorf("BackupRoot = %q, want %q", b.BackupRoot, root)
	}
	if b.probePath == "" {
		t.Error("probePath should not be empty")
	}
}

// --- List ---

func TestList_EmptyDir(t *testing.T) {
	b, root := newBackend(t)
	os.MkdirAll(filepath.Join(root, "ns"), 0o755)
	files, err := b.List("ns")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestList_MissingNamespace(t *testing.T) {
	b, _ := newBackend(t)
	files, err := b.List("no/such/ns")
	if err != nil {
		t.Fatalf("unexpected error for missing namespace: %v", err)
	}
	if files != nil {
		t.Errorf("expected nil, got %v", files)
	}
}

func TestList_WithFiles(t *testing.T) {
	b, root := newBackend(t)
	ns := "edge/inst/job"
	writeFile(t, filepath.Join(root, ns, "a.tar.zst"), "aaa")
	writeFile(t, filepath.Join(root, ns, "b.tar.zst"), "bb")

	files, err := b.List(ns)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	sizes := map[string]int64{}
	for _, f := range files {
		sizes[f.Filename] = f.SizeBytes
	}
	if sizes["a.tar.zst"] != 3 || sizes["b.tar.zst"] != 2 {
		t.Errorf("unexpected sizes: %v", sizes)
	}
}

func TestList_SkipsDirectories(t *testing.T) {
	b, root := newBackend(t)
	ns := "edge/inst/job"
	os.MkdirAll(filepath.Join(root, ns, "subdir"), 0o755)
	writeFile(t, filepath.Join(root, ns, "file.tar.zst"), "data")

	files, err := b.List(ns)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 file (subdir skipped), got %d", len(files))
	}
}

func TestList_FilesHavePaths(t *testing.T) {
	b, root := newBackend(t)
	ns := "edge/inst/job"
	writeFile(t, filepath.Join(root, ns, "a.tar.zst"), "data")

	files, _ := b.List(ns)
	if files[0].Path == "" {
		t.Error("Path should be set")
	}
	if files[0].Mtime == 0 {
		t.Error("Mtime should be set")
	}
}

// --- Store ---

func TestStore_SameFilesystem(t *testing.T) {
	b, root := newBackend(t)
	// Create a staged file in same dir (same FS)
	staged := filepath.Join(root, "staged.bin")
	writeFile(t, staged, "payload data")

	storedAs, err := b.Store("edge/inst/job", "backup.tar.zst", staged)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if storedAs != "backup.tar.zst" {
		t.Errorf("storedAs = %q, want backup.tar.zst", storedAs)
	}
	content, err := os.ReadFile(filepath.Join(root, "edge/inst/job", "backup.tar.zst"))
	if err != nil {
		t.Fatalf("reading stored file: %v", err)
	}
	if string(content) != "payload data" {
		t.Errorf("content mismatch: %q", content)
	}
}

func TestStore_CreatesNamespaceDir(t *testing.T) {
	b, root := newBackend(t)
	staged := filepath.Join(root, "staged.bin")
	writeFile(t, staged, "data")

	_, err := b.Store("new/deep/ns", "file.tar.zst", staged)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "new/deep/ns")); err != nil {
		t.Error("namespace dir should be created")
	}
}

// --- Delete ---

func TestDelete_Existing(t *testing.T) {
	b, root := newBackend(t)
	ns := "edge/inst/job"
	writeFile(t, filepath.Join(root, ns, "a.tar.zst"), "data")

	if err := b.Delete(ns, "a.tar.zst"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ns, "a.tar.zst")); !os.IsNotExist(err) {
		t.Error("file should be deleted")
	}
}

func TestDelete_Missing(t *testing.T) {
	b, _ := newBackend(t)
	// Deleting a nonexistent file should return nil (not an error)
	if err := b.Delete("ns", "nonexistent.tar.zst"); err != nil {
		t.Errorf("Delete missing file should return nil, got %v", err)
	}
}

// --- Healthcheck ---

func TestHealthcheck_ValidRoot(t *testing.T) {
	b, _ := newBackend(t)
	if !b.Healthcheck() {
		t.Error("Healthcheck should return true for valid backup root")
	}
}

func TestHealthcheck_MissingRoot(t *testing.T) {
	b := NewLocalBackend("/nonexistent/backup/root/xyz")
	if b.Healthcheck() {
		t.Error("Healthcheck should return false for missing root")
	}
}

// --- copyAcrossFilesystems ---

func TestCopyAcrossFilesystems(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")
	writeFile(t, src, "copy me")

	if err := copyAcrossFilesystems(src, dst); err != nil {
		t.Fatalf("copyAcrossFilesystems: %v", err)
	}
	content, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading dst: %v", err)
	}
	if string(content) != "copy me" {
		t.Errorf("content = %q", content)
	}
	// Source should be removed
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source should be removed after cross-fs copy")
	}
}

func TestCopyAcrossFilesystems_MissingSrc(t *testing.T) {
	dir := t.TempDir()
	err := copyAcrossFilesystems("/nonexistent/src", filepath.Join(dir, "dst"))
	if err == nil {
		t.Error("expected error for missing source")
	}
}

// --- DirSize ---

func TestDirSize(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a"), "hello")
	writeFile(t, filepath.Join(dir, "b"), "world!")

	got := DirSize(dir)
	if got != 11 {
		t.Errorf("DirSize = %d, want 11", got)
	}
}

func TestDirSize_Empty(t *testing.T) {
	dir := t.TempDir()
	if got := DirSize(dir); got != 0 {
		t.Errorf("DirSize empty = %d, want 0", got)
	}
}

func TestDirSize_Nested(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "sub", "file"), "abc")
	if got := DirSize(dir); got != 3 {
		t.Errorf("DirSize nested = %d, want 3", got)
	}
}

// --- isCrossDeviceError ---

func TestIsCrossDeviceError_NonLinkError(t *testing.T) {
	// A plain error is not a cross-device error
	err := os.ErrNotExist
	if isCrossDeviceError(err) {
		t.Error("os.ErrNotExist should not be a cross-device error")
	}
}

func TestIsCrossDeviceError_LinkErrorNotCrossDevice(t *testing.T) {
	// A LinkError with a non-EXDEV inner error is not a cross-device error
	err := &os.LinkError{Op: "rename", Old: "a", New: "b", Err: os.ErrPermission}
	if isCrossDeviceError(err) {
		t.Error("permission error LinkError should not be cross-device")
	}
}

func TestIsCrossDeviceError_Nil(t *testing.T) {
	if isCrossDeviceError(nil) {
		t.Error("nil should not be a cross-device error")
	}
}

// --- copyAcrossFilesystems error paths ---

func TestCopyAcrossFilesystems_DstDirMissing(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	writeFile(t, src, "data")

	err := copyAcrossFilesystems(src, "/nonexistent/path/to/dst.bin")
	if err == nil {
		t.Error("expected error when dst directory doesn't exist")
	}
}

func TestCopyAcrossFilesystems_DstIsDirectory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	writeFile(t, src, "data")

	// Make dst path a directory so the final Rename fails
	dstDir := filepath.Join(dir, "dstdir")
	os.MkdirAll(dstDir, 0o755)

	err := copyAcrossFilesystems(src, dstDir)
	if err == nil {
		t.Error("expected error when dst is a directory")
	}
}

// --- Healthcheck when probePath is a directory ---

func TestHealthcheck_ProbePathIsDirectory(t *testing.T) {
	root := t.TempDir()
	b := NewLocalBackend(root)
	// Create a directory at the probePath location
	os.MkdirAll(b.probePath, 0o755)
	// Healthcheck should fail when it can't open probePath as a file
	if b.Healthcheck() {
		t.Error("Healthcheck should fail when probePath is a directory")
	}
}

// --- Healthcheck edge cases ---

func TestHealthcheck_RootIsFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "notadir")
	writeFile(t, filePath, "not a directory")
	b := NewLocalBackend(filePath)
	if b.Healthcheck() {
		t.Error("Healthcheck should fail when BackupRoot is a file, not a dir")
	}
}

// --- DiskUsage ---

func TestDiskUsage(t *testing.T) {
	dir := t.TempDir()
	total, used, free, err := DiskUsage(dir)
	if err != nil {
		t.Fatalf("DiskUsage: %v", err)
	}
	if total <= 0 || free <= 0 || used < 0 {
		t.Errorf("DiskUsage unexpected values: total=%d used=%d free=%d", total, used, free)
	}
	if total < used+free {
		t.Errorf("total (%d) < used (%d) + free (%d)", total, used, free)
	}
}
