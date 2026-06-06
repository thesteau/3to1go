package services

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxHookFiles       = 3
	hookTimeoutSeconds = 300
)

var allowedHookSuffixes = map[string]bool{".sh": true, ".txt": true}

type HookManager struct {
	ScriptsDir string
	logger     *slog.Logger
}

func NewHookManager(scriptsDir string, logger *slog.Logger) *HookManager {
	os.MkdirAll(scriptsDir, 0o755)
	return &HookManager{ScriptsDir: scriptsDir, logger: logger}
}

type HookFileInfo struct {
	Name       string `json:"name"`
	SizeBytes  int64  `json:"size_bytes"`
	ModifiedAt string `json:"modified_at"`
	Viewable   bool   `json:"viewable"`
}

func (h *HookManager) Snapshot(preCommand, postCommand string) map[string]any {
	return map[string]any{
		"pre_command":  preCommand,
		"post_command": postCommand,
		"script_dir":   h.ScriptsDir,
		"max_files":    MaxHookFiles,
		"files":        h.ListFiles(),
	}
}

func (h *HookManager) ListFiles() []HookFileInfo {
	os.MkdirAll(h.ScriptsDir, 0o755)
	entries, _ := os.ReadDir(h.ScriptsDir)
	var files []HookFileInfo
	for _, e := range entries {
		if e.IsDir() || len(files) >= MaxHookFiles {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, hookFileInfo(filepath.Join(h.ScriptsDir, e.Name()), info))
	}
	return files
}

func (h *HookManager) SaveUploadedFile(filename string, content []byte) (HookFileInfo, error) {
	safeName := sanitizeFilename(filename)
	if safeName == "" {
		return HookFileInfo{}, fmt.Errorf("filename is required")
	}
	ext := strings.ToLower(filepath.Ext(safeName))
	if !allowedHookSuffixes[ext] {
		return HookFileInfo{}, fmt.Errorf("only .sh scripts or .txt helper files are allowed")
	}
	text := string(content)
	if !isUTF8(content) {
		return HookFileInfo{}, fmt.Errorf("only UTF-8 text files are allowed")
	}

	existing := h.existingNames()
	if _, known := existing[safeName]; !known && len(existing) >= MaxHookFiles {
		return HookFileInfo{}, fmt.Errorf("only the first 3 files are supported here")
	}

	if ext == ".sh" {
		text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	}

	target := filepath.Join(h.ScriptsDir, safeName)
	if err := os.WriteFile(target, []byte(text), 0o644); err != nil {
		return HookFileInfo{}, err
	}
	if ext == ".sh" {
		info, _ := os.Stat(target)
		if info != nil {
			os.Chmod(target, info.Mode()|0o700)
		}
	}
	info, err := os.Stat(target)
	if err != nil {
		return HookFileInfo{}, err
	}
	return hookFileInfo(target, info), nil
}

func (h *HookManager) ReadTextFile(filename string) (string, string, error) {
	safeName := sanitizeFilename(filename)
	path := filepath.Join(h.ScriptsDir, safeName)
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", "", fmt.Errorf("%s: not found", safeName)
	}
	if err != nil {
		return "", "", err
	}
	if !isUTF8(content) {
		return "", "", fmt.Errorf("this file cannot be viewed because it is not text")
	}
	return safeName, string(content), nil
}

func (h *HookManager) DeleteFile(filename string) error {
	safeName := sanitizeFilename(filename)
	path := filepath.Join(h.ScriptsDir, safeName)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("%s: not found", safeName)
	}
	return err
}

func (h *HookManager) RunCommand(command, phase string, hookCtx map[string]any) {
	normalized := strings.TrimSpace(command)
	if normalized == "" {
		return
	}

	shellCmd := h.resolveCommand(normalized)
	env := os.Environ()
	env = append3to1goEnv(env, "APP", "central")
	env = append3to1goEnv(env, "HOOK_PHASE", phase)
	env = append3to1goEnv(env, "HOOK_SCRIPTS_DIR", h.ScriptsDir)
	for k, v := range hookCtx {
		val := ""
		if v != nil {
			val = fmt.Sprintf("%v", v)
		}
		env = append3to1goEnv(env, strings.ToUpper(k), val)
	}

	ctx, cancel := context.WithTimeout(context.Background(), hookTimeoutSeconds*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", shellCmd)
	cmd.Dir = h.ScriptsDir
	cmd.Env = env

	stdout, runErr := cmd.Output()
	var stderr []byte
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		stderr = exitErr.Stderr
	}
	if ctx.Err() == context.DeadlineExceeded {
		h.logger.Warn("hook_execution_timeout", "phase", phase, "command", normalized)
		return
	}

	if len(stdout) > 0 {
		h.logger.Info("hook_execution_stdout", "phase", phase, "command", normalized, "output", strings.TrimSpace(string(stdout)))
	}
	if len(stderr) > 0 {
		h.logger.Warn("hook_execution_stderr", "phase", phase, "command", normalized, "output", strings.TrimSpace(string(stderr)))
	}
	if runErr != nil {
		h.logger.Warn("hook_execution_nonzero", "phase", phase, "command", normalized, "error", runErr)
	}
}

func append3to1goEnv(env []string, name, value string) []string {
	return append(env, "THREETOONEGO_"+name+"="+value)
}

func (h *HookManager) resolveCommand(command string) string {
	if strings.ContainsAny(command, " \t") {
		return command
	}
	candidate := filepath.Join(h.ScriptsDir, command)
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return "./" + command
	}
	return command
}

func (h *HookManager) existingNames() map[string]struct{} {
	entries, _ := os.ReadDir(h.ScriptsDir)
	m := make(map[string]struct{})
	for _, e := range entries {
		if !e.IsDir() {
			m[e.Name()] = struct{}{}
		}
	}
	return m
}

func hookFileInfo(path string, info os.FileInfo) HookFileInfo {
	content, err := os.ReadFile(path)
	viewable := err == nil && isUTF8(content)
	return HookFileInfo{
		Name:       info.Name(),
		SizeBytes:  info.Size(),
		ModifiedAt: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		Viewable:   viewable,
	}
}

func sanitizeFilename(filename string) string {
	base := filepath.Base(strings.TrimSpace(filename))
	if base == "." || base == ".." {
		return ""
	}
	return base
}

func isUTF8(data []byte) bool {
	return utf8.Valid(data)
}
