package encryption

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateRejectsMalformedKeyWithoutReplacingIt(t *testing.T) {
	for _, size := range []int{0, 16, 31, 33} {
		path := filepath.Join(t.TempDir(), "encryption.key")
		original := bytes.Repeat([]byte{42}, size)
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOrCreate(path); err == nil {
			t.Fatalf("accepted %d-byte key", size)
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, original) {
			t.Fatalf("existing key changed: %v", err)
		}
	}
}

func TestLoadOrCreateReturnsReadFailure(t *testing.T) {
	path := t.TempDir()
	if _, err := LoadOrCreate(path); err == nil {
		t.Fatal("accepted a directory as a key")
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatal("key path was changed")
	}
}
