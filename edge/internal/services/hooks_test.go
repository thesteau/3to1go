package services

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// HookManager
// ---------------------------------------------------------------------------

func newTestHookManager(t *testing.T) *HookManager {
	t.Helper()
	return NewHookManager(t.TempDir(), discardSlogLogger())
}

func TestHookManager_ListFiles_Empty(t *testing.T) {
	h := newTestHookManager(t)
	files := h.ListFiles()
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestHookManager_SaveUploadedFile_ShScript(t *testing.T) {
	h := newTestHookManager(t)
	content := []byte("#!/bin/sh\necho hello\n")
	info, err := h.SaveUploadedFile("pre.sh", content)
	if err != nil {
		t.Fatalf("SaveUploadedFile: %v", err)
	}
	if info.Name != "pre.sh" {
		t.Errorf("Name = %q, want pre.sh", info.Name)
	}
	if info.SizeBytes == 0 {
		t.Error("expected non-zero file size")
	}
}

func TestHookManager_SaveUploadedFile_TxtFile(t *testing.T) {
	h := newTestHookManager(t)
	info, err := h.SaveUploadedFile("config.txt", []byte("key=value\n"))
	if err != nil {
		t.Fatalf("SaveUploadedFile: %v", err)
	}
	if info.Name != "config.txt" {
		t.Errorf("Name = %q, want config.txt", info.Name)
	}
}

func TestHookManager_SaveUploadedFile_BadExtension(t *testing.T) {
	h := newTestHookManager(t)
	_, err := h.SaveUploadedFile("evil.exe", []byte("MZ"))
	if err == nil {
		t.Error("expected error for disallowed extension")
	}
}

func TestHookManager_SaveUploadedFile_NonUTF8(t *testing.T) {
	h := newTestHookManager(t)
	_, err := h.SaveUploadedFile("pre.sh", []byte{0xFF, 0xFE, 0x00})
	if err == nil {
		t.Error("expected error for non-UTF8 content")
	}
}

func TestHookManager_SaveUploadedFile_MaxFilesExceeded(t *testing.T) {
	h := newTestHookManager(t)
	// Fill up to the limit.
	for i := range MaxHookFiles {
		name := filepath.Join(h.ScriptsDir, "file"+string(rune('a'+i))+".sh")
		os.WriteFile(name, []byte("#!/bin/sh\n"), 0o644)
	}
	_, err := h.SaveUploadedFile("extra.sh", []byte("#!/bin/sh\n"))
	if err == nil {
		t.Errorf("expected error when exceeding max files (%d)", MaxHookFiles)
	}
}

func TestHookManager_ReadTextFile_Success(t *testing.T) {
	h := newTestHookManager(t)
	h.SaveUploadedFile("pre.sh", []byte("#!/bin/sh\necho hello\n"))
	name, content, err := h.ReadTextFile("pre.sh")
	if err != nil {
		t.Fatalf("ReadTextFile: %v", err)
	}
	if name != "pre.sh" {
		t.Errorf("name = %q, want pre.sh", name)
	}
	if content == "" {
		t.Error("expected non-empty content")
	}
}

func TestHookManager_ReadTextFile_NotFound(t *testing.T) {
	h := newTestHookManager(t)
	_, _, err := h.ReadTextFile("missing.sh")
	if err == nil {
		t.Error("expected error for missing file")
	}
	// Error message should end with ": not found".
	if len(err.Error()) < 9 || err.Error()[len(err.Error())-9:] != "not found" {
		t.Errorf("error message %q should end with 'not found'", err.Error())
	}
}

func TestHookManager_DeleteFile_Success(t *testing.T) {
	h := newTestHookManager(t)
	h.SaveUploadedFile("pre.sh", []byte("#!/bin/sh\n"))
	if err := h.DeleteFile("pre.sh"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	// File should be gone.
	if _, err := os.Stat(filepath.Join(h.ScriptsDir, "pre.sh")); !os.IsNotExist(err) {
		t.Error("file should not exist after delete")
	}
}

func TestHookManager_DeleteFile_NotFound(t *testing.T) {
	h := newTestHookManager(t)
	err := h.DeleteFile("missing.sh")
	if err == nil {
		t.Error("expected error for missing file")
	}
	if len(err.Error()) < 9 || err.Error()[len(err.Error())-9:] != "not found" {
		t.Errorf("error message %q should end with 'not found'", err.Error())
	}
}

func TestHookManager_Snapshot_Structure(t *testing.T) {
	h := newTestHookManager(t)
	snap := h.Snapshot("echo pre", "echo post")
	if snap["pre_command"] != "echo pre" {
		t.Errorf("pre_command = %v", snap["pre_command"])
	}
	if snap["post_command"] != "echo post" {
		t.Errorf("post_command = %v", snap["post_command"])
	}
	if snap["max_files"] == nil {
		t.Error("expected max_files in snapshot")
	}
}

func TestHookManager_ListFiles_ReturnsViewable(t *testing.T) {
	h := newTestHookManager(t)
	h.SaveUploadedFile("pre.sh", []byte("#!/bin/sh\n"))
	files := h.ListFiles()
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if !files[0].Viewable {
		t.Error("expected .sh file to be viewable")
	}
}
