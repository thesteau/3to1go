package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeDirectoriesExcludedAutomatically(t *testing.T) {
	root := t.TempDir()
	write := func(name string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("config/key")
	write("spool/pending.tar.zst")
	write("state/nested/progress")
	write("user/spool/keep")
	write("user/state/keep")
	for _, name := range []string{"spool", "state"} {
		if err := MarkRuntimeDir(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	job := &JobDefinition{RootPath: root, IncludeHidden: true, FollowSymlinks: true}
	files, err := BuildFileList(job, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files, want config and two unmarked user files", len(files))
	}
	for i, want := range []string{"config/key", "user/spool/keep", "user/state/keep"} {
		if files[i].ArchivePath != want {
			t.Fatalf("got %q, want %q", files[i].ArchivePath, want)
		}
	}
	job.RootPath = filepath.Join(root, "state", "nested")
	files, err = BuildFileList(job, nil)
	if err != nil || len(files) != 0 {
		t.Fatalf("runtime subtree: files=%v err=%v", files, err)
	}
	write("state/nested/" + UploadDirFilename)
	jobs, err := DiscoverJobs(root, 5, nil)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("runtime jobs discovered: %v, err=%v", jobs, err)
	}
}

func TestRuntimeSymlinkExcluded(t *testing.T) {
	runtimeDir := t.TempDir()
	if err := MarkRuntimeDir(runtimeDir); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(runtimeDir, "pending")
	if err := os.WriteFile(file, []byte("archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for name, target := range map[string]string{"directory": runtimeDir, "file": file} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	files, err := BuildFileList(&JobDefinition{RootPath: root, IncludeHidden: true, FollowSymlinks: true}, nil)
	if err != nil || len(files) != 0 {
		t.Fatalf("runtime symlinks: files=%v err=%v", files, err)
	}
}
