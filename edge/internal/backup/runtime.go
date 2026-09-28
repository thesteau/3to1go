package backup

import (
	"os"
	"path/filepath"
)

// RuntimeMarker identifies disposable Edge runtime data, never backup content.
const RuntimeMarker = ".3to1go-runtime"

// MarkRuntimeDir creates an internal directory and marks it for automatic exclusion.
func MarkRuntimeDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, RuntimeMarker), []byte("3to1go Edge runtime data; excluded from backups.\n"), 0o644)
}

func isRuntimePath(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	for {
		if info, err := os.Stat(filepath.Join(abs, RuntimeMarker)); err == nil && info.Mode().IsRegular() {
			return true
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return false
		}
		abs = parent
	}
}
