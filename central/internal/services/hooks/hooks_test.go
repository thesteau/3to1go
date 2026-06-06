package hooks

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newHookManager(t *testing.T) (*HookManager, string) {
	t.Helper()
	dir := t.TempDir()
	hm := NewHookManager(dir, discardLogger())
	return hm, dir
}

// --- sanitizeFilename ---

func TestSanitizeFilename_Basic(t *testing.T) {
	cases := []struct{ in, want string }{
		{"script.sh", "script.sh"},
		{"/etc/passwd", "passwd"},
		{"../escape.sh", "escape.sh"},
		{"  name.txt  ", "name.txt"},
		{".", ""},
		{"..", ""},
	}
	for _, c := range cases {
		got := sanitizeFilename(c.in)
		if got != c.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- SaveUploadedFile ---

func TestSaveUploadedFile_EmptyFilename(t *testing.T) {
	hm, _ := newHookManager(t)
	_, err := hm.SaveUploadedFile("", []byte("content"))
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("expected required error, got %v", err)
	}
}

func TestSaveUploadedFile_InvalidExtension(t *testing.T) {
	hm, _ := newHookManager(t)
	_, err := hm.SaveUploadedFile("script.py", []byte("content"))
	if err == nil || !strings.Contains(err.Error(), ".sh") {
		t.Errorf("expected extension error, got %v", err)
	}
}

func TestSaveUploadedFile_NonUTF8(t *testing.T) {
	hm, _ := newHookManager(t)
	_, err := hm.SaveUploadedFile("script.sh", []byte{0xff, 0xfe})
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Errorf("expected UTF-8 error, got %v", err)
	}
}

func TestSaveUploadedFile_ShScript_CRLF(t *testing.T) {
	hm, dir := newHookManager(t)
	content := "#!/bin/sh\r\necho hello\r\n"
	info, err := hm.SaveUploadedFile("run.sh", []byte(content))
	if err != nil {
		t.Fatalf("SaveUploadedFile: %v", err)
	}
	if info.Name != "run.sh" {
		t.Errorf("Name = %q", info.Name)
	}
	saved, _ := os.ReadFile(filepath.Join(dir, "run.sh"))
	if strings.Contains(string(saved), "\r") {
		t.Error("CRLF should be normalized for .sh files")
	}
}

func TestSaveUploadedFile_TxtFile_CRLFPreserved(t *testing.T) {
	hm, dir := newHookManager(t)
	content := "line1\r\nline2\r\n"
	_, err := hm.SaveUploadedFile("helper.txt", []byte(content))
	if err != nil {
		t.Fatalf("SaveUploadedFile: %v", err)
	}
	saved, _ := os.ReadFile(filepath.Join(dir, "helper.txt"))
	if !strings.Contains(string(saved), "\r\n") {
		t.Error("CRLF should be preserved for .txt files")
	}
}

func TestSaveUploadedFile_MaxFilesLimit(t *testing.T) {
	hm, _ := newHookManager(t)
	// Fill to max
	for i := range MaxHookFiles {
		name := strings.Repeat("x", i+1) + ".sh"
		hm.SaveUploadedFile(name, []byte("#!/bin/sh\necho ok\n"))
	}
	// One more different file should fail
	_, err := hm.SaveUploadedFile("extra.sh", []byte("#!/bin/sh\necho hi\n"))
	if err == nil || !strings.Contains(err.Error(), "3") {
		t.Errorf("expected limit error, got %v", err)
	}
}

func TestSaveUploadedFile_OverwriteExisting(t *testing.T) {
	hm, _ := newHookManager(t)
	// Fill to max
	for i := range MaxHookFiles {
		name := strings.Repeat("x", i+1) + ".sh"
		hm.SaveUploadedFile(name, []byte("#!/bin/sh\necho ok\n"))
	}
	// Overwriting an existing file at max should succeed
	_, err := hm.SaveUploadedFile("x.sh", []byte("#!/bin/sh\necho updated\n"))
	if err != nil {
		t.Errorf("overwriting existing file should succeed: %v", err)
	}
}

// --- ListFiles ---

func TestListFiles_Empty(t *testing.T) {
	hm, _ := newHookManager(t)
	files := hm.ListFiles()
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestListFiles_WithFiles(t *testing.T) {
	hm, _ := newHookManager(t)
	hm.SaveUploadedFile("run.sh", []byte("#!/bin/sh\necho hi\n"))
	hm.SaveUploadedFile("helper.txt", []byte("notes"))

	files := hm.ListFiles()
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
}

func TestListFiles_SkipsDirectories(t *testing.T) {
	hm, dir := newHookManager(t)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0o755)
	hm.SaveUploadedFile("run.sh", []byte("#!/bin/sh\n"))

	files := hm.ListFiles()
	if len(files) != 1 {
		t.Errorf("expected 1 file (dir skipped), got %d", len(files))
	}
}

func TestListFiles_Viewable(t *testing.T) {
	hm, _ := newHookManager(t)
	hm.SaveUploadedFile("run.sh", []byte("#!/bin/sh\necho hi\n"))
	files := hm.ListFiles()
	if len(files) == 0 || !files[0].Viewable {
		t.Error("text file should be viewable")
	}
}

// --- DeleteFile ---

func TestDeleteFile_Existing(t *testing.T) {
	hm, _ := newHookManager(t)
	hm.SaveUploadedFile("run.sh", []byte("#!/bin/sh\necho hi\n"))
	if err := hm.DeleteFile("run.sh"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if len(hm.ListFiles()) != 0 {
		t.Error("file should be deleted")
	}
}

func TestDeleteFile_Missing(t *testing.T) {
	hm, _ := newHookManager(t)
	err := hm.DeleteFile("nonexistent.sh")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found error, got %v", err)
	}
}

// --- ReadTextFile ---

func TestReadTextFile_Existing(t *testing.T) {
	hm, _ := newHookManager(t)
	hm.SaveUploadedFile("helper.txt", []byte("some notes"))
	name, content, err := hm.ReadTextFile("helper.txt")
	if err != nil {
		t.Fatalf("ReadTextFile: %v", err)
	}
	if name != "helper.txt" || content != "some notes" {
		t.Errorf("got (%q, %q)", name, content)
	}
}

func TestReadTextFile_Missing(t *testing.T) {
	hm, _ := newHookManager(t)
	_, _, err := hm.ReadTextFile("nope.txt")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestReadTextFile_BinaryFile(t *testing.T) {
	hm, dir := newHookManager(t)
	// Write binary directly (bypassing SaveUploadedFile validation)
	os.WriteFile(filepath.Join(dir, "binary.txt"), []byte{0xff, 0xfe, 0x00}, 0o644)
	_, _, err := hm.ReadTextFile("binary.txt")
	if err == nil || !strings.Contains(err.Error(), "not text") {
		t.Errorf("expected not text error, got %v", err)
	}
}

// --- Snapshot ---

func TestHookManagerSnapshot(t *testing.T) {
	hm, _ := newHookManager(t)
	snap := hm.Snapshot("pre.sh", "post.sh")
	if snap["pre_command"] != "pre.sh" {
		t.Errorf("snapshot missing pre_command: %v", snap)
	}
	if snap["max_files"] != MaxHookFiles {
		t.Errorf("snapshot missing max_files")
	}
}

// --- resolveCommand ---

func TestResolveCommand_WithSpaces(t *testing.T) {
	hm, _ := newHookManager(t)
	// Commands with spaces are returned as-is
	got := hm.resolveCommand("sh -c 'echo hello'")
	if got != "sh -c 'echo hello'" {
		t.Errorf("got %q", got)
	}
}

func TestResolveCommand_FileInScriptsDir(t *testing.T) {
	hm, dir := newHookManager(t)
	os.WriteFile(filepath.Join(dir, "myscript.sh"), []byte("#!/bin/sh"), 0o755)
	got := hm.resolveCommand("myscript.sh")
	if got != "./myscript.sh" {
		t.Errorf("got %q, want ./myscript.sh", got)
	}
}

func TestResolveCommand_NotInScriptsDir(t *testing.T) {
	hm, _ := newHookManager(t)
	got := hm.resolveCommand("somecommand")
	if got != "somecommand" {
		t.Errorf("got %q, want somecommand", got)
	}
}
