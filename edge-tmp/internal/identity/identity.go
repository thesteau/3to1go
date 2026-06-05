package identity

import (
	"os"
	"path/filepath"
	"strings"

	"crypto/rand"
	"encoding/hex"
)

// LoadOrCreate reads the installation ID from path, creating it if absent.
func LoadOrCreate(path string) string {
	if data, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id
		}
	}
	id := randomHex(16)
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(id+"\n"), 0o600)
	return id
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
