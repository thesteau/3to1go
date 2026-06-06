package services

import (
	"os"
	"path/filepath"
	"testing"
)

const fakePEM = "-----BEGIN CERTIFICATE-----\nMIIBIjANBgkqfakecert\n-----END CERTIFICATE-----\n"

func newTestCertManager(t *testing.T) *CertManager {
	t.Helper()
	return &CertManager{
		StorageDir:     t.TempDir(),
		TrustTargetDir: t.TempDir(),
		UpdateCommand:  "true", // no-op command available on Linux/macOS
	}
}

// ---------------------------------------------------------------------------
// CertManager
// ---------------------------------------------------------------------------

func TestCertManager_ListFiles_Empty(t *testing.T) {
	c := newTestCertManager(t)
	files := c.ListFiles()
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestCertManager_SaveUploadedFile_ValidPEM(t *testing.T) {
	c := newTestCertManager(t)
	info, err := c.SaveUploadedFile("ca.crt", []byte(fakePEM))
	if err != nil {
		t.Fatalf("SaveUploadedFile: %v", err)
	}
	if info.Name != "ca.crt" {
		t.Errorf("Name = %q, want ca.crt", info.Name)
	}
	if info.SizeBytes == 0 {
		t.Error("expected non-zero file size")
	}
}

func TestCertManager_SaveUploadedFile_BadExtension(t *testing.T) {
	c := newTestCertManager(t)
	_, err := c.SaveUploadedFile("cert.pem", []byte(fakePEM))
	if err == nil {
		t.Error("expected error for .pem extension (only .crt allowed)")
	}
}

func TestCertManager_SaveUploadedFile_NotPEM(t *testing.T) {
	c := newTestCertManager(t)
	_, err := c.SaveUploadedFile("ca.crt", []byte("not a pem file"))
	if err == nil {
		t.Error("expected error for non-PEM content")
	}
}

func TestCertManager_SaveUploadedFile_NonUTF8(t *testing.T) {
	c := newTestCertManager(t)
	_, err := c.SaveUploadedFile("ca.crt", []byte{0xFF, 0xFE})
	if err == nil {
		t.Error("expected error for non-UTF8 content")
	}
}

func TestCertManager_SaveUploadedFile_EmptyFilename(t *testing.T) {
	c := newTestCertManager(t)
	_, err := c.SaveUploadedFile("", []byte(fakePEM))
	if err == nil {
		t.Error("expected error for empty filename")
	}
}

func TestCertManager_SaveUploadedFile_MaxFilesExceeded(t *testing.T) {
	c := newTestCertManager(t)
	// Fill to the limit.
	for i := range MaxCertificateFiles {
		name := filepath.Join(c.StorageDir, "ca"+string(rune('a'+i))+".crt")
		os.WriteFile(name, []byte(fakePEM), 0o644)
	}
	_, err := c.SaveUploadedFile("extra.crt", []byte(fakePEM))
	if err == nil {
		t.Errorf("expected error when exceeding max files (%d)", MaxCertificateFiles)
	}
}

func TestCertManager_DeleteFile_Success(t *testing.T) {
	c := newTestCertManager(t)
	c.SaveUploadedFile("ca.crt", []byte(fakePEM))
	if err := c.DeleteFile("ca.crt"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.StorageDir, "ca.crt")); !os.IsNotExist(err) {
		t.Error("cert file should not exist after delete")
	}
}

func TestCertManager_DeleteFile_NotFound(t *testing.T) {
	c := newTestCertManager(t)
	err := c.DeleteFile("missing.crt")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestCertManager_Snapshot_Structure(t *testing.T) {
	c := newTestCertManager(t)
	snap := c.Snapshot()
	if snap["cert_dir"] == nil {
		t.Error("expected cert_dir in snapshot")
	}
	if snap["max_files"] == nil {
		t.Error("expected max_files in snapshot")
	}
	if snap["files"] == nil {
		t.Error("expected files in snapshot")
	}
}

func TestCertManager_TLSConfig_NoCerts_ReturnsNil(t *testing.T) {
	c := newTestCertManager(t)
	if c.TLSConfig() != nil {
		t.Error("expected nil TLSConfig when no certs stored")
	}
}

func TestCertManager_ListFiles_AfterSave(t *testing.T) {
	c := newTestCertManager(t)
	c.SaveUploadedFile("ca.crt", []byte(fakePEM))
	files := c.ListFiles()
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Name != "ca.crt" {
		t.Errorf("Name = %q, want ca.crt", files[0].Name)
	}
}
