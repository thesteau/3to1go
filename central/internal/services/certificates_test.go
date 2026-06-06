package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validPEM = `-----BEGIN CERTIFICATE-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA2a fake cert for testing
-----END CERTIFICATE-----
`

func newCertManager(t *testing.T) (*CertManager, string) {
	t.Helper()
	storageDir := t.TempDir()
	trustDir := filepath.Join(t.TempDir(), "trust")
	os.MkdirAll(trustDir, 0o755)
	return &CertManager{
		StorageDir:     storageDir,
		TrustTargetDir: trustDir,
		UpdateCommand:  "true", // requires sh; skip if not available
	}, storageDir
}

// --- ListFiles ---

func TestCertListFiles_Empty(t *testing.T) {
	cm, _ := newCertManager(t)
	files := cm.ListFiles()
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestCertListFiles_SkipsDirectories(t *testing.T) {
	cm, dir := newCertManager(t)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0o755)
	writeFile(t, filepath.Join(dir, "cert.crt"), validPEM)
	files := cm.ListFiles()
	if len(files) != 1 {
		t.Errorf("expected 1 file, got %d", len(files))
	}
}

func TestCertListFiles_MaxFiles(t *testing.T) {
	cm, dir := newCertManager(t)
	for i := range MaxCertificateFiles + 2 {
		name := strings.Repeat("x", i+1) + ".crt"
		writeFile(t, filepath.Join(dir, name), validPEM)
	}
	files := cm.ListFiles()
	if len(files) != MaxCertificateFiles {
		t.Errorf("expected %d files, got %d", MaxCertificateFiles, len(files))
	}
}

// --- SaveUploadedFile validation ---

func TestCertSave_EmptyFilename(t *testing.T) {
	cm, _ := newCertManager(t)
	_, err := cm.SaveUploadedFile("", []byte(validPEM))
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("expected required error, got %v", err)
	}
}

func TestCertSave_WrongExtension(t *testing.T) {
	cm, _ := newCertManager(t)
	_, err := cm.SaveUploadedFile("cert.pem", []byte(validPEM))
	if err == nil || !strings.Contains(err.Error(), ".crt") {
		t.Errorf("expected extension error, got %v", err)
	}
}

func TestCertSave_NonUTF8(t *testing.T) {
	cm, _ := newCertManager(t)
	_, err := cm.SaveUploadedFile("cert.crt", []byte{0xff, 0xfe, 0x00})
	if err == nil || !strings.Contains(err.Error(), "PEM") {
		t.Errorf("expected UTF-8/PEM error, got %v", err)
	}
}

func TestCertSave_NotPEM(t *testing.T) {
	cm, _ := newCertManager(t)
	_, err := cm.SaveUploadedFile("cert.crt", []byte("this is not a certificate"))
	if err == nil || !strings.Contains(err.Error(), "PEM") {
		t.Errorf("expected PEM error, got %v", err)
	}
}

func TestCertSave_MissingBegin(t *testing.T) {
	cm, _ := newCertManager(t)
	content := "-----END CERTIFICATE-----\n"
	_, err := cm.SaveUploadedFile("cert.crt", []byte(content))
	if err == nil || !strings.Contains(err.Error(), "PEM") {
		t.Errorf("expected PEM error, got %v", err)
	}
}

func TestCertSave_PathTraversal(t *testing.T) {
	cm, _ := newCertManager(t)
	// ../escape.crt should be sanitized to escape.crt and then validation proceeds normally
	_, err := cm.SaveUploadedFile("../escape.crt", []byte("not pem"))
	if err == nil || !strings.Contains(err.Error(), "PEM") {
		// The filename gets sanitized to "escape.crt" but content is invalid PEM
		t.Errorf("expected PEM content error, got %v", err)
	}
}

func TestCertSave_MaxFilesLimit(t *testing.T) {
	cm, dir := newCertManager(t)
	for i := range MaxCertificateFiles {
		name := strings.Repeat("a", i+1) + ".crt"
		writeFile(t, filepath.Join(dir, name), validPEM)
	}
	_, err := cm.SaveUploadedFile("extra.crt", []byte(validPEM))
	if err == nil || !strings.Contains(err.Error(), "10") {
		t.Errorf("expected limit error, got %v", err)
	}
}

// --- DeleteFile validation ---

func TestCertDeleteFile_Missing(t *testing.T) {
	cm, _ := newCertManager(t)
	err := cm.DeleteFile("nonexistent.crt")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found error, got %v", err)
	}
}

// --- Snapshot ---

func TestCertManagerSnapshot(t *testing.T) {
	cm, _ := newCertManager(t)
	snap := cm.Snapshot()
	if snap["max_files"] != MaxCertificateFiles {
		t.Errorf("snapshot max_files missing: %v", snap)
	}
	if snap["cert_dir"] != cm.StorageDir {
		t.Errorf("snapshot cert_dir missing")
	}
}

// --- ExecError ---

func TestExecError_Error(t *testing.T) {
	e := &ExecError{msg: "something failed"}
	if e.Error() != "something failed" {
		t.Errorf("got %q", e.Error())
	}
}
