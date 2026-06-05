package services

import (
	"os"
	"path/filepath"
	"testing"
)

// Tests that require sh (available on Linux/macOS/Docker Alpine).

func newCertManagerWithShell(t *testing.T) *CertManager {
	t.Helper()
	hasSh(t)
	storageDir := t.TempDir()
	trustDir := filepath.Join(t.TempDir(), "trust")
	os.MkdirAll(trustDir, 0o755)
	return &CertManager{
		StorageDir:     storageDir,
		TrustTargetDir: trustDir,
		UpdateCommand:  "true", // sh -c true — always succeeds
	}
}

func TestCertSave_Success(t *testing.T) {
	cm := newCertManagerWithShell(t)
	info, err := cm.SaveUploadedFile("mycert.crt", []byte(validPEM))
	if err != nil {
		t.Fatalf("SaveUploadedFile: %v", err)
	}
	if info.Name != "mycert.crt" {
		t.Errorf("Name = %q", info.Name)
	}
	// File should appear in ListFiles
	files := cm.ListFiles()
	if len(files) != 1 || files[0].Name != "mycert.crt" {
		t.Errorf("ListFiles = %v", files)
	}
}

func TestCertSave_OverwriteExisting(t *testing.T) {
	cm := newCertManagerWithShell(t)
	// Fill to max
	for i := 0; i < MaxCertificateFiles; i++ {
		name := string(rune('a'+i)) + ".crt"
		writeFile(t, filepath.Join(cm.StorageDir, name), validPEM)
	}
	// Overwriting existing should succeed
	_, err := cm.SaveUploadedFile("a.crt", []byte(validPEM))
	if err != nil {
		t.Errorf("overwriting existing cert should succeed: %v", err)
	}
}

func TestCertSave_CRLFNormalized(t *testing.T) {
	cm := newCertManagerWithShell(t)
	content := "-----BEGIN CERTIFICATE-----\r\nMIIB\r\n-----END CERTIFICATE-----\r\n"
	_, err := cm.SaveUploadedFile("cert.crt", []byte(content))
	if err != nil {
		t.Fatalf("SaveUploadedFile: %v", err)
	}
	saved, _ := os.ReadFile(filepath.Join(cm.StorageDir, "cert.crt"))
	// CRLF should be normalized
	for i, b := range saved {
		if b == '\r' {
			t.Errorf("CRLF not normalized at byte %d", i)
			break
		}
	}
}

func TestCertDelete_Success(t *testing.T) {
	cm := newCertManagerWithShell(t)
	cm.SaveUploadedFile("del.crt", []byte(validPEM))
	if err := cm.DeleteFile("del.crt"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if len(cm.ListFiles()) != 0 {
		t.Error("file should be deleted")
	}
}

func TestNewCertManager_EnvVars(t *testing.T) {
	hasSh(t)
	t.Setenv("RELAY_TRUST_TARGET_DIR", "/tmp/test-trust")
	t.Setenv("RELAY_UPDATE_CA_CERTIFICATES", "true")
	cm := NewCertManager(t.TempDir())
	if cm.TrustTargetDir != "/tmp/test-trust" {
		t.Errorf("TrustTargetDir = %q", cm.TrustTargetDir)
	}
	if cm.UpdateCommand != "true" {
		t.Errorf("UpdateCommand = %q", cm.UpdateCommand)
	}
}

func TestNewCertManager_Defaults(t *testing.T) {
	hasSh(t)
	t.Setenv("RELAY_TRUST_TARGET_DIR", "")
	t.Setenv("RELAY_UPDATE_CA_CERTIFICATES", "")
	cm := NewCertManager(t.TempDir())
	if cm.TrustTargetDir == "" {
		t.Error("TrustTargetDir should have default")
	}
	if cm.UpdateCommand == "" {
		t.Error("UpdateCommand should have default")
	}
}

func TestUpdateTrustStore_Failure(t *testing.T) {
	hasSh(t)
	storageDir := t.TempDir()
	trustDir := filepath.Join(t.TempDir(), "trust")
	os.MkdirAll(trustDir, 0o755)
	cm := &CertManager{
		StorageDir:     storageDir,
		TrustTargetDir: trustDir,
		UpdateCommand:  "exit 1", // always fails
	}
	err := cm.updateTrustStore()
	if err == nil {
		t.Error("expected error when update command fails")
	}
	if _, ok := err.(*ExecError); !ok {
		t.Errorf("expected ExecError, got %T: %v", err, err)
	}
}
