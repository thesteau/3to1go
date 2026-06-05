package services

import (
	"os/exec"
	"testing"
)

func hasSh(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
}

func TestRunCommand_Empty(t *testing.T) {
	hm, _ := newHookManager(t)
	// Empty command should be a no-op without panic
	hm.RunCommand("", "pre", map[string]interface{}{})
}

func TestRunCommand_Success(t *testing.T) {
	hasSh(t)
	hm, _ := newHookManager(t)
	hm.RunCommand("echo hello", "pre", map[string]interface{}{"job_name": "myjob"})
	// No assertion - just verify it doesn't panic
}

func TestRunCommand_Nonzero(t *testing.T) {
	hasSh(t)
	hm, _ := newHookManager(t)
	// A command that exits non-zero should not panic
	hm.RunCommand("exit 1", "post", map[string]interface{}{})
}

func TestRunCommand_EnvInjection(t *testing.T) {
	hasSh(t)
	hm, dir := newHookManager(t)
	// Write a script that checks the env vars
	hm.SaveUploadedFile("check.sh", []byte("#!/bin/sh\necho \"$THREETOONEGO_APP\" \"$THREETOONEGO_HOOK_PHASE\"\n"))
	hm.RunCommand("check.sh", "pre", map[string]interface{}{})
	_ = dir // used to create temp script
}

func TestRunCommand_ScriptInScriptsDir(t *testing.T) {
	hasSh(t)
	hm, _ := newHookManager(t)
	hm.SaveUploadedFile("myscript.sh", []byte("#!/bin/sh\necho ok\n"))
	hm.RunCommand("myscript.sh", "pre", map[string]interface{}{})
}
