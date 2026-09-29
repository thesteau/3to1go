package backup

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestArchiveRestoresFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support POSIX permission bits")
	}
	for _, mode := range []os.FileMode{0o600, 0o640, 0o755} {
		root := t.TempDir()
		source := filepath.Join(root, "source")
		if err := os.WriteFile(source, []byte("backup data"), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(source, mode); err != nil {
			t.Fatal(err)
		}
		archive := filepath.Join(root, "archive.tar.zst")
		if err := CreateArchive(archive, []*DiscoveredFile{{SourcePath: source, ArchivePath: "restored"}}); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "target")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		// Replacement must restore the archived mode, not retain the old mode.
		dest := filepath.Join(target, "restored")
		if err := os.WriteFile(dest, []byte("old"), 0o666); err != nil {
			t.Fatal(err)
		}
		if _, err := ExtractArchive(archive, target); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(dest)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("restored mode %o, want %o", info.Mode().Perm(), mode)
		}
		data, err := os.ReadFile(dest)
		if err != nil || string(data) != "backup data" {
			t.Fatalf("restored data %q: %v", data, err)
		}
	}
}
