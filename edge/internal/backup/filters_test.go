package backup

import (
	"os"
	"path/filepath"
	"testing"
)

// --- containsHidden ---

func TestContainsHidden_NoHidden(t *testing.T) {
	if containsHidden("docs/readme.txt") {
		t.Error("expected no hidden component in docs/readme.txt")
	}
}

func TestContainsHidden_DotFile(t *testing.T) {
	if !containsHidden(".bashrc") {
		t.Error("expected .bashrc to be hidden")
	}
}

func TestContainsHidden_HiddenParentDir(t *testing.T) {
	if !containsHidden(".ssh/config") {
		t.Error("expected .ssh/config to be hidden via parent .ssh")
	}
}

func TestContainsHidden_HiddenInMiddle(t *testing.T) {
	if !containsHidden("docs/.cache/readme.txt") {
		t.Error("expected .cache in middle of path to be flagged hidden")
	}
}

func TestContainsHidden_DeepNested(t *testing.T) {
	if containsHidden("a/b/c/d.txt") {
		t.Error("expected no hidden in a/b/c/d.txt")
	}
}

// --- matchesExclude ---

func TestMatchesExclude_NoPatterns(t *testing.T) {
	if matchesExclude("any/file.txt", nil) {
		t.Error("expected no match with nil patterns")
	}
	if matchesExclude("any/file.txt", []string{}) {
		t.Error("expected no match with empty patterns")
	}
}

func TestMatchesExclude_GlobByBasename(t *testing.T) {
	if !matchesExclude("path/to/file.log", []string{"*.log"}) {
		t.Error("expected *.log to match file.log")
	}
	if matchesExclude("path/to/file.txt", []string{"*.log"}) {
		t.Error("expected *.log not to match file.txt")
	}
}

func TestMatchesExclude_DirectoryTrailingSlash(t *testing.T) {
	if !matchesExclude("tmp/data.bin", []string{"tmp/"}) {
		t.Error("expected tmp/ to exclude tmp/data.bin")
	}
	if !matchesExclude("tmp", []string{"tmp/"}) {
		t.Error("expected tmp/ to exclude path equal to prefix")
	}
	if matchesExclude("notmp/data.bin", []string{"tmp/"}) {
		t.Error("expected tmp/ not to match notmp/data.bin")
	}
}

func TestMatchesExclude_NestedDirectoryPattern(t *testing.T) {
	if !matchesExclude("a/node_modules/b.js", []string{"node_modules/"}) {
		t.Error("expected node_modules/ to match a/node_modules/b.js")
	}
}

func TestMatchesExclude_PathPattern(t *testing.T) {
	if !matchesExclude("build/output.txt", []string{"build/*.txt"}) {
		t.Error("expected build/*.txt to match build/output.txt")
	}
	if matchesExclude("src/output.txt", []string{"build/*.txt"}) {
		t.Error("expected build/*.txt not to match src/output.txt")
	}
}

func TestMatchesExclude_EmptyPatternSkipped(t *testing.T) {
	if matchesExclude("file.txt", []string{""}) {
		t.Error("empty pattern should not match")
	}
	if matchesExclude("file.txt", []string{"   "}) {
		t.Error("whitespace-only pattern should not match")
	}
}

func TestMatchesExclude_MultiplePatterns(t *testing.T) {
	patterns := []string{"*.log", "*.tmp", "build/"}
	if !matchesExclude("app.log", patterns) {
		t.Error("expected *.log to match")
	}
	if !matchesExclude("cache.tmp", patterns) {
		t.Error("expected *.tmp to match")
	}
	if !matchesExclude("build/out.bin", patterns) {
		t.Error("expected build/ to match build/out.bin")
	}
	if matchesExclude("main.go", patterns) {
		t.Error("expected main.go not to match any pattern")
	}
}

// --- BuildFileList ---

func TestBuildFileList_ReturnsFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644)
	job := &JobDefinition{RootPath: dir, JobName: "test", IncludeHidden: true}
	files, err := BuildFileList(job, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("got %d files, want 2", len(files))
	}
}

func TestBuildFileList_ExcludesUploadDirMarker(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, UploadDirFilename), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "data.txt"), []byte("x"), 0o644)
	job := &JobDefinition{RootPath: dir, JobName: "test", IncludeHidden: true}
	files, err := BuildFileList(job, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range files {
		if f.ArchivePath == UploadDirFilename {
			t.Errorf("expected %s to be excluded from file list", UploadDirFilename)
		}
	}
}

func TestBuildFileList_ExcludesHiddenWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o644)
	job := &JobDefinition{RootPath: dir, JobName: "test", IncludeHidden: false}
	files, err := BuildFileList(job, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0].ArchivePath != "visible.txt" {
		t.Errorf("expected only visible.txt, got %v", files)
	}
}

func TestBuildFileList_IncludesHiddenWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o644)
	job := &JobDefinition{RootPath: dir, JobName: "test", IncludeHidden: true}
	files, err := BuildFileList(job, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files (including hidden), got %d", len(files))
	}
}

func TestBuildFileList_SortedByArchivePath(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"c.txt", "a.txt", "b.txt"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	job := &JobDefinition{RootPath: dir, JobName: "test", IncludeHidden: false}
	files, err := BuildFileList(job, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3", len(files))
	}
	for i, want := range []string{"a.txt", "b.txt", "c.txt"} {
		if files[i].ArchivePath != want {
			t.Errorf("files[%d] = %q, want %q", i, files[i].ArchivePath, want)
		}
	}
}

func TestBuildFileList_AppliesExcludePatterns(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "temp.log"), []byte("x"), 0o644)
	job := &JobDefinition{
		RootPath:        dir,
		JobName:         "test",
		IncludeHidden:   false,
		ExcludePatterns: []string{"*.log"},
	}
	files, err := BuildFileList(job, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0].ArchivePath != "keep.txt" {
		t.Errorf("expected only keep.txt, got %v", files)
	}
}

func TestBuildFileList_RecursesSubdirectories(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	os.WriteFile(filepath.Join(dir, "root.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(sub, "child.txt"), []byte("x"), 0o644)
	job := &JobDefinition{RootPath: dir, JobName: "test", IncludeHidden: false}
	files, err := BuildFileList(job, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files from recursive walk, got %d", len(files))
	}
	paths := map[string]bool{}
	for _, f := range files {
		paths[f.ArchivePath] = true
	}
	if !paths["root.txt"] || !paths["sub/child.txt"] {
		t.Errorf("unexpected file paths: %v", paths)
	}
}

func TestBuildFileList_ExcludesSubdirectoryByPattern(t *testing.T) {
	dir := t.TempDir()
	buildDir := filepath.Join(dir, "build")
	os.Mkdir(buildDir, 0o755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(buildDir, "output"), []byte("x"), 0o644)
	job := &JobDefinition{
		RootPath:        dir,
		JobName:         "test",
		IncludeHidden:   false,
		ExcludePatterns: []string{"build/"},
	}
	files, err := BuildFileList(job, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0].ArchivePath != "main.go" {
		t.Errorf("expected only main.go, got %v", files)
	}
}
