package backup

import (
	"os"
	"path/filepath"
	"testing"
)

// --- BuildJobDefinition ---

func TestBuildJobDefinition_DefaultsToBasename(t *testing.T) {
	dir := t.TempDir()
	job, err := BuildJobDefinition(dir, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When no job_name is provided, falls back to directory basename.
	abs, _ := filepath.Abs(dir)
	if job.JobName != filepath.Base(abs) {
		t.Errorf("job name = %q, want %q", job.JobName, filepath.Base(abs))
	}
}

func TestBuildJobDefinition_CustomJobName(t *testing.T) {
	dir := t.TempDir()
	job, err := BuildJobDefinition(dir, map[string]any{"job_name": "my-backup"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.JobName != "my-backup" {
		t.Errorf("job name = %q, want my-backup", job.JobName)
	}
}

func TestBuildJobDefinition_InvalidJobName_Spaces(t *testing.T) {
	dir := t.TempDir()
	if _, err := BuildJobDefinition(dir, map[string]any{"job_name": "has spaces"}); err == nil {
		t.Error("expected error for job_name with spaces")
	}
}

func TestBuildJobDefinition_InvalidJobName_Slash(t *testing.T) {
	dir := t.TempDir()
	if _, err := BuildJobDefinition(dir, map[string]any{"job_name": "has/slash"}); err == nil {
		t.Error("expected error for job_name with slash")
	}
}

func TestBuildJobDefinition_InvalidJobName_AtSign(t *testing.T) {
	dir := t.TempDir()
	if _, err := BuildJobDefinition(dir, map[string]any{"job_name": "at@sign"}); err == nil {
		t.Error("expected error for job_name with @")
	}
}

func TestBuildJobDefinition_ExcludePatterns(t *testing.T) {
	dir := t.TempDir()
	job, err := BuildJobDefinition(dir, map[string]any{
		"exclude": []any{"*.log", "tmp/"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(job.ExcludePatterns) != 2 {
		t.Errorf("got %d exclude patterns, want 2", len(job.ExcludePatterns))
	}
}

func TestBuildJobDefinition_ExcludeNotList(t *testing.T) {
	dir := t.TempDir()
	if _, err := BuildJobDefinition(dir, map[string]any{"exclude": "not-a-list"}); err == nil {
		t.Error("expected error when exclude is a string, not a list")
	}
}

func TestBuildJobDefinition_IncludeHiddenDefault(t *testing.T) {
	dir := t.TempDir()
	job, err := BuildJobDefinition(dir, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !job.IncludeHidden {
		t.Error("expected IncludeHidden to default to true")
	}
}

func TestBuildJobDefinition_IncludeHiddenFalse(t *testing.T) {
	dir := t.TempDir()
	job, err := BuildJobDefinition(dir, map[string]any{"include_hidden": false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.IncludeHidden {
		t.Error("expected IncludeHidden to be false when explicitly set")
	}
}

func TestBuildJobDefinition_FollowSymlinksDefault(t *testing.T) {
	dir := t.TempDir()
	job, err := BuildJobDefinition(dir, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.FollowSymlinks {
		t.Error("expected FollowSymlinks to default to false")
	}
}

func TestBuildJobDefinition_FollowSymlinksTrue(t *testing.T) {
	dir := t.TempDir()
	job, err := BuildJobDefinition(dir, map[string]any{"follow_symlinks": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !job.FollowSymlinks {
		t.Error("expected FollowSymlinks to be true when explicitly set")
	}
}

func TestBuildJobDefinition_AbsoluteRootPath(t *testing.T) {
	dir := t.TempDir()
	job, err := BuildJobDefinition(dir, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(job.RootPath) {
		t.Errorf("expected absolute RootPath, got %q", job.RootPath)
	}
}

// --- ReadUploadDirPayload ---

func TestReadUploadDirPayload_EmptyFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.upload_dir")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	payload, err := ReadUploadDirPayload(f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(payload) != 0 {
		t.Errorf("expected empty payload, got %v", payload)
	}
}

func TestReadUploadDirPayload_WhitespaceOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".upload_dir")
	if err := os.WriteFile(path, []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err := ReadUploadDirPayload(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(payload) != 0 {
		t.Errorf("expected empty payload for whitespace-only file, got %v", payload)
	}
}

func TestReadUploadDirPayload_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".upload_dir")
	content := "job_name: photos\nexclude:\n  - '*.tmp'\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err := ReadUploadDirPayload(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payload["job_name"] != "photos" {
		t.Errorf("job_name = %v, want photos", payload["job_name"])
	}
}

func TestReadUploadDirPayload_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".upload_dir")
	if err := os.WriteFile(path, []byte(":\t{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadUploadDirPayload(path); err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestReadUploadDirPayload_MissingFile(t *testing.T) {
	if _, err := ReadUploadDirPayload("/nonexistent/.upload_dir"); err == nil {
		t.Error("expected error for missing file")
	}
}

// --- DiscoverJobs ---

func TestDiscoverJobs_FindsMarkerAtRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, UploadDirFilename), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	jobs, err := DiscoverJobs(dir, 2, func(string, ...any) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("got %d jobs, want 1", len(jobs))
	}
}

func TestDiscoverJobs_FindsMarkerInSubdir(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "mydata")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, UploadDirFilename), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	jobs, err := DiscoverJobs(root, 2, func(string, ...any) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("got %d jobs, want 1", len(jobs))
	}
	if filepath.Base(jobs[0].RootPath) != "mydata" {
		t.Errorf("job root = %q, want mydata subdir", jobs[0].RootPath)
	}
}

func TestDiscoverJobs_MaxDepthRespected(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, UploadDirFilename), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	// maxDepth=1: marker is at depth 2, should not be found
	jobs, err := DiscoverJobs(root, 1, func(string, ...any) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("maxDepth=1: got %d jobs, want 0", len(jobs))
	}
	// maxDepth=2: should find it
	jobs, err = DiscoverJobs(root, 2, func(string, ...any) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("maxDepth=2: got %d jobs, want 1", len(jobs))
	}
}

func TestDiscoverJobs_MultipleJobs(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"photos", "documents", "music"} {
		sub := filepath.Join(root, name)
		os.Mkdir(sub, 0o755)
		os.WriteFile(filepath.Join(sub, UploadDirFilename), []byte(""), 0o644)
	}
	jobs, err := DiscoverJobs(root, 2, func(string, ...any) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 3 {
		t.Errorf("got %d jobs, want 3", len(jobs))
	}
}

func TestDiscoverJobs_NoMarkers(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "sub1", "sub2"), 0o755)
	jobs, err := DiscoverJobs(root, 5, func(string, ...any) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("got %d jobs, want 0", len(jobs))
	}
}

func TestDiscoverJobs_StopsRecursingAtMarker(t *testing.T) {
	// A marker at root/sub means root/sub/nested should not be scanned for another marker.
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	nested := filepath.Join(sub, "nested")
	os.MkdirAll(nested, 0o755)
	os.WriteFile(filepath.Join(sub, UploadDirFilename), []byte(""), 0o644)
	os.WriteFile(filepath.Join(nested, UploadDirFilename), []byte(""), 0o644)

	jobs, err := DiscoverJobs(root, 5, func(string, ...any) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the outer marker should be returned; recursion stops at the first marker.
	if len(jobs) != 1 {
		t.Errorf("got %d jobs, want 1 (should not recurse past a marker)", len(jobs))
	}
}

// --- JobDefinitionToPayload / WriteUploadDir round-trip ---

func TestJobDefinitionToPayload_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	job, err := BuildJobDefinition(dir, map[string]any{
		"job_name":        "backup-job",
		"exclude":         []any{"*.tmp"},
		"include_hidden":  false,
		"follow_symlinks": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	payload := JobDefinitionToPayload(job)
	if payload["job_name"] != "backup-job" {
		t.Errorf("job_name = %v, want backup-job", payload["job_name"])
	}
	if payload["follow_symlinks"] != true {
		t.Errorf("follow_symlinks = %v, want true", payload["follow_symlinks"])
	}
}

func TestWriteUploadDir_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	err := WriteUploadDir(dir, map[string]any{"job_name": "test-write"})
	if err != nil {
		t.Fatalf("WriteUploadDir: %v", err)
	}
	markerPath := filepath.Join(dir, UploadDirFilename)
	if _, err := os.Stat(markerPath); err != nil {
		t.Errorf("expected %s to exist: %v", UploadDirFilename, err)
	}
}
