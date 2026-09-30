package hooks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestApplicationEnvironmentAndCancellation(t *testing.T) {
	hasSh(t)
	for _, app := range []string{"central", "edge"} {
		t.Run(app, func(t *testing.T) {
			dir := t.TempDir()
			manager := NewHookManager(app, dir, discardLogger())
			manager.RunCommand(`printf '%s' "$THREETOONEGO_APP" > app.txt`, "pre", nil)
			data, err := os.ReadFile(filepath.Join(dir, "app.txt"))
			if err != nil || string(data) != app {
				t.Fatalf("application environment: %q, %v", data, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			manager.RunCommandContext(ctx, "touch canceled.txt", "pre", nil)
			if _, err := os.Stat(filepath.Join(dir, "canceled.txt")); !os.IsNotExist(err) {
				t.Fatalf("canceled hook executed: %v", err)
			}
		})
	}
}
