package certificates

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxCertificateFiles = 10
	certSuffix          = ".crt"
)

// CertManager stores trusted TLS certificates and installs them into the system trust store.
type CertManager struct {
	StorageDir     string
	TrustTargetDir string
	UpdateCommand  string
}

// CertFileInfo describes a single stored certificate file.
type CertFileInfo struct {
	Name       string `json:"name"`
	SizeBytes  int64  `json:"size_bytes"`
	ModifiedAt string `json:"modified_at"`
}

func NewCertManager(storageDir string) *CertManager {
	trustTarget := strings.TrimSpace(os.Getenv("THREETOONEGO_TRUST_TARGET_DIR"))
	if trustTarget == "" {
		trustTarget = "/usr/local/share/ca-certificates/3to1go"
	}
	updateCmd := strings.TrimSpace(os.Getenv("THREETOONEGO_UPDATE_CA_CERTIFICATES"))
	if updateCmd == "" {
		updateCmd = "update-ca-certificates"
	}
	os.MkdirAll(storageDir, 0o755)
	return &CertManager{
		StorageDir:     storageDir,
		TrustTargetDir: trustTarget,
		UpdateCommand:  updateCmd,
	}
}

func (c *CertManager) Snapshot() map[string]any {
	return map[string]any{
		"cert_dir":  c.StorageDir,
		"max_files": MaxCertificateFiles,
		"files":     c.ListFiles(),
	}
}

func (c *CertManager) ListFiles() []CertFileInfo {
	os.MkdirAll(c.StorageDir, 0o755)
	entries, _ := os.ReadDir(c.StorageDir)
	var files []CertFileInfo
	for _, e := range entries {
		if e.IsDir() || len(files) >= MaxCertificateFiles {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, CertFileInfo{
			Name:       e.Name(),
			SizeBytes:  info.Size(),
			ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return files
}

func (c *CertManager) SaveUploadedFile(filename string, content []byte) (CertFileInfo, error) {
	safeName := sanitizeFilename(filename)
	if safeName == "" {
		return CertFileInfo{}, fmt.Errorf("filename is required")
	}
	if strings.ToLower(filepath.Ext(safeName)) != certSuffix {
		return CertFileInfo{}, fmt.Errorf("only .crt certificate files are allowed")
	}
	if !utf8.Valid(content) {
		return CertFileInfo{}, fmt.Errorf("certificate must be UTF-8 PEM text")
	}
	text := strings.ReplaceAll(strings.ReplaceAll(string(content), "\r\n", "\n"), "\r", "\n")
	if !strings.Contains(text, "-----BEGIN CERTIFICATE-----") || !strings.Contains(text, "-----END CERTIFICATE-----") {
		return CertFileInfo{}, fmt.Errorf("certificate must be PEM encoded")
	}

	existing := c.existingNames()
	if _, known := existing[safeName]; !known && len(existing) >= MaxCertificateFiles {
		return CertFileInfo{}, fmt.Errorf("only the first 10 certificate files are supported here")
	}

	target := filepath.Join(c.StorageDir, safeName)
	if err := os.WriteFile(target, []byte(text), 0o644); err != nil {
		return CertFileInfo{}, err
	}
	if err := c.installTrustFile(target); err != nil {
		return CertFileInfo{}, err
	}
	if err := c.updateTrustStore(); err != nil {
		return CertFileInfo{}, err
	}
	info, _ := os.Stat(target)
	return CertFileInfo{Name: safeName, SizeBytes: info.Size(), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339)}, nil
}

func (c *CertManager) DeleteFile(filename string) error {
	safeName := sanitizeFilename(filename)
	path := filepath.Join(c.StorageDir, safeName)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: not found", safeName)
		}
		return err
	}
	os.Remove(filepath.Join(c.TrustTargetDir, safeName))
	return c.updateTrustStore()
}

// tlsConfig builds a *tls.Config that trusts the stored certificates in addition to system roots.
func (c *CertManager) tlsConfig() *tls.Config {
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	entries, _ := os.ReadDir(c.StorageDir)
	added := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		pem, err := os.ReadFile(filepath.Join(c.StorageDir, e.Name()))
		if err == nil && pool.AppendCertsFromPEM(pem) {
			added = true
		}
	}
	if !added {
		return nil
	}
	return &tls.Config{RootCAs: pool}
}

// TLSConfig returns a *tls.Config with stored certs appended, or nil if none stored.
func (c *CertManager) TLSConfig() *tls.Config {
	return c.tlsConfig()
}

func (c *CertManager) installTrustFile(src string) error {
	os.MkdirAll(c.TrustTargetDir, 0o755)
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.TrustTargetDir, filepath.Base(src)), content, 0o644)
}

// ExecError wraps an OS-command failure.
type ExecError struct {
	msg string
	Err error
}

func (e *ExecError) Error() string { return e.msg }
func (e *ExecError) Unwrap() error { return e.Err }

func (c *CertManager) updateTrustStore() error {
	cmd := exec.Command("sh", "-c", c.UpdateCommand)
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = fmt.Sprintf("%s failed", c.UpdateCommand)
		}
		return &ExecError{msg: detail, Err: err}
	}
	return nil
}

func (c *CertManager) existingNames() map[string]struct{} {
	entries, _ := os.ReadDir(c.StorageDir)
	m := make(map[string]struct{})
	for _, e := range entries {
		if !e.IsDir() {
			m[e.Name()] = struct{}{}
		}
	}
	return m
}

func sanitizeFilename(filename string) string {
	base := filepath.Base(strings.TrimSpace(filename))
	if base == "." || base == ".." {
		return ""
	}
	return base
}
