package identity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"crypto/rand"
	"encoding/hex"
)

// LoadOrCreate reads the installation ID from path, creating it if absent.
func LoadOrCreate(path string) string {
	id, err := LoadOrCreateChecked(path)
	if err != nil {
		return ""
	}
	return id
}

// LoadOrCreateChecked reads or atomically creates the installation ID.
func LoadOrCreateChecked(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id, nil
		}
	}
	id, err := randomHex(16)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create identity directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".installation-id-*")
	if err != nil {
		return "", fmt.Errorf("create identity file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.WriteString(id+"\n"); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		if data, readErr := os.ReadFile(path); readErr == nil {
			if existing := strings.TrimSpace(string(data)); existing != "" {
				return existing, nil
			}
		}
		return "", fmt.Errorf("persist identity: %w", err)
	}
	return id, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate identity: %w", err)
	}
	return hex.EncodeToString(b), nil
}
