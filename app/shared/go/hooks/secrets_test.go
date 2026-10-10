package hooks

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestHookLogsOmitCommandsAndOutput(t *testing.T) {
	hasSh(t)
	var logs bytes.Buffer
	m := NewHookManager("scout", t.TempDir(), slog.New(slog.NewTextHandler(&logs, nil)))
	m.RunCommand("printf 'secret-stdout'; printf 'secret-stderr' >&2; exit 1 # secret-command", "post", nil)
	if strings.Contains(logs.String(), "secret-") {
		t.Fatalf("hook exposed secret output: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "hook_execution_nonzero") {
		t.Fatal("failure outcome missing")
	}
}
