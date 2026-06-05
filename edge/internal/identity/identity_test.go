package identity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreate_CreatesFileIfAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id.txt")
	id := LoadOrCreate(path)
	if id == "" {
		t.Fatal("expected non-empty ID")
	}
	if len(id) != 32 {
		t.Errorf("expected 32-char hex ID, got %q (len %d)", id, len(id))
	}
	// File must now exist.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file to be created: %v", err)
	}
	if strings.TrimSpace(string(data)) != id {
		t.Errorf("file content %q does not match returned ID %q", strings.TrimSpace(string(data)), id)
	}
}

func TestLoadOrCreate_ReturnsSameIDOnSecondCall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id.txt")
	id1 := LoadOrCreate(path)
	id2 := LoadOrCreate(path)
	if id1 != id2 {
		t.Errorf("first call = %q, second call = %q (must be equal)", id1, id2)
	}
}

func TestLoadOrCreate_ReadsExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id.txt")
	want := "deadbeefdeadbeefdeadbeefdeadbeef"
	os.WriteFile(path, []byte(want+"\n"), 0o600)
	got := LoadOrCreate(path)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLoadOrCreate_CreatesParentDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "nested", "id.txt")
	id := LoadOrCreate(path)
	if id == "" {
		t.Fatal("expected non-empty ID")
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("parent dir not created: %v", err)
	}
}

func TestLoadOrCreate_IgnoresEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id.txt")
	os.WriteFile(path, []byte("   \n"), 0o600)
	id := LoadOrCreate(path)
	if id == "" {
		t.Fatal("expected new ID when file is whitespace-only")
	}
	// Should have overwritten the file with a real ID.
	data, _ := os.ReadFile(path)
	if strings.TrimSpace(string(data)) != id {
		t.Errorf("file was not updated with new ID")
	}
}

func TestRandomHex_CorrectLength(t *testing.T) {
	for _, n := range []int{8, 16, 32} {
		got := randomHex(n)
		if len(got) != n*2 {
			t.Errorf("randomHex(%d): len=%d, want %d", n, len(got), n*2)
		}
	}
}

func TestRandomHex_Unique(t *testing.T) {
	a := randomHex(16)
	b := randomHex(16)
	if a == b {
		t.Error("two randomHex calls should not produce identical output")
	}
}
