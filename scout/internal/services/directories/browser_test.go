package directories

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/3to1go/scout/internal/backup"
)

func TestBrowseExcludeAndSizes(t *testing.T) {
	root := t.TempDir()
	svc, _ := newDirService(t, root)
	svc.settings.MaxDepth = 0 // browsing must still reach nested files
	for _, name := range []string{"nested/file[1].txt", "nested/file1.txt", "other/nested/file[1].txt", "empty"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		content := []byte("12345")
		if name == "empty" {
			content = nil
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeMarker(t, root, map[string]any{"job_name": "self", "exclude": []any{"*.tmp"}, "include_hidden": true})
	entries, err := svc.Browse("nested")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Size != 5 || entries[0].Kind != "file" || entries[0].JobPath != "." {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	for range 2 {
		if err := svc.ExcludePath("nested/file[1].txt"); err != nil {
			t.Fatal(err)
		}
	}
	job, err := svc.LoadJob(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(job.ExcludePatterns) != 2 || job.ExcludePatterns[1] != "/nested/file[1].txt" {
		t.Fatalf("patterns: %v", job.ExcludePatterns)
	}
	files, err := backup.BuildFileList(job, nil)
	if err != nil || len(files) != 3 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	entries, err = svc.Browse("nested")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Excluded != (e.Name == "file[1].txt") {
			t.Fatalf("incorrect exclusion: %+v", e)
		}
	}
	if err := svc.ExcludePath("nested"); err != nil {
		t.Fatal(err)
	}
	job, _ = svc.LoadJob(".")
	files, err = backup.BuildFileList(job, nil)
	if err != nil || len(files) != 2 {
		t.Fatalf("directory exclusion: files=%v err=%v", files, err)
	}
	if _, err := os.Stat(filepath.Join(root, "nested", backup.UploadDirFilename)); !os.IsNotExist(err) {
		t.Fatal("created nested marker")
	}
	size, err := svc.FolderSize(context.Background(), "nested")
	if err != nil || size["size"] != int64(10) || size["files"] != int64(2) {
		t.Fatalf("size=%v err=%v", size, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.FolderSize(ctx, "."); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestBrowserRejectsEscapesAndSkipsRuntime(t *testing.T) {
	root := t.TempDir()
	svc, _ := newDirService(t, root)
	if _, err := svc.Browse("../"); err == nil {
		t.Fatal("allowed traversal")
	}
	if err := svc.ExcludePath("../"); err == nil {
		t.Fatal("allowed traversal exclusion")
	}
	runtime := filepath.Join(root, "spool")
	if err := backup.MarkRuntimeDir(runtime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtime, "pending"), []byte("archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Browse("spool"); err == nil {
		t.Fatal("browsed runtime")
	}
	size, err := svc.FolderSize(context.Background(), ".")
	if err != nil || size["size"] != int64(0) {
		t.Fatalf("runtime counted: %v %v", size, err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "outside")); err != nil {
		t.Skip(err)
	}
	if _, err := svc.Browse("outside"); err == nil {
		t.Fatal("allowed external symlink")
	}
}
