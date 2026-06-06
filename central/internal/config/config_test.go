package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helpers to isolate env vars
func setEnv(t *testing.T, key, val string) {
	t.Helper()
	t.Setenv(key, val)
}

func clearEnvKeys(t *testing.T, keys ...string) {
	for _, k := range keys {
		t.Setenv(k, "")
	}
}

// --- coerceInt ---

func TestCoerceInt_EmptyReturnsDefault(t *testing.T) {
	if got := coerceInt("", 42, 0); got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestCoerceInt_ValidValue(t *testing.T) {
	if got := coerceInt("10", 0, 0); got != 10 {
		t.Errorf("got %d, want 10", got)
	}
}

func TestCoerceInt_InvalidReturnsDefault(t *testing.T) {
	if got := coerceInt("abc", 5, 0); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
}

func TestCoerceInt_BelowMinClamped(t *testing.T) {
	if got := coerceInt("1", 10, 5); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
}

func TestCoerceInt_TrimSpace(t *testing.T) {
	if got := coerceInt("  20  ", 0, 0); got != 20 {
		t.Errorf("got %d, want 20", got)
	}
}

// --- coerceText ---

func TestCoerceText_EmptyReturnsDefault(t *testing.T) {
	if got := coerceText("", "default"); got != "default" {
		t.Errorf("got %q, want default", got)
	}
}

func TestCoerceText_WhitespaceReturnsDefault(t *testing.T) {
	if got := coerceText("   ", "default"); got != "default" {
		t.Errorf("got %q, want default", got)
	}
}

func TestCoerceText_ValueTrimmed(t *testing.T) {
	if got := coerceText("  hello  ", "x"); got != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

// --- coerceURL ---

func TestCoerceURL_EmptyReturnsEmpty(t *testing.T) {
	v, err := coerceURL("")
	if err != nil || v != "" {
		t.Errorf("got (%q, %v), want ('', nil)", v, err)
	}
}

func TestCoerceURL_ValidHTTP(t *testing.T) {
	v, err := coerceURL("http://example.com")
	if err != nil || v != "http://example.com" {
		t.Errorf("got (%q, %v)", v, err)
	}
}

func TestCoerceURL_ValidHTTPS(t *testing.T) {
	v, err := coerceURL("https://ntfy.sh/topic")
	if err != nil || v != "https://ntfy.sh/topic" {
		t.Errorf("got (%q, %v)", v, err)
	}
}

func TestCoerceURL_TrailingSlashStripped(t *testing.T) {
	v, err := coerceURL("https://example.com/")
	if err != nil || v != "https://example.com" {
		t.Errorf("got (%q, %v)", v, err)
	}
}

func TestCoerceURL_InvalidScheme(t *testing.T) {
	_, err := coerceURL("ftp://example.com")
	if err == nil {
		t.Error("expected error for ftp scheme")
	}
}

func TestCoerceURL_NoHost(t *testing.T) {
	_, err := coerceURL("http://")
	if err == nil {
		t.Error("expected error for URL with no host")
	}
}

func TestCoerceURL_NotAURL(t *testing.T) {
	_, err := coerceURL("not-a-url")
	if err == nil {
		t.Error("expected error for non-URL string")
	}
}

// --- coerceTheme ---

func TestCoerceTheme_LightLower(t *testing.T) {
	if got := coerceTheme("light"); got != "light" {
		t.Errorf("got %q, want light", got)
	}
}

func TestCoerceTheme_LightUpper(t *testing.T) {
	if got := coerceTheme("LIGHT"); got != "light" {
		t.Errorf("got %q, want light", got)
	}
}

func TestCoerceTheme_Dark(t *testing.T) {
	if got := coerceTheme("dark"); got != "dark" {
		t.Errorf("got %q, want dark", got)
	}
}

func TestCoerceTheme_UnknownDefaultsDark(t *testing.T) {
	if got := coerceTheme("solarized"); got != "dark" {
		t.Errorf("got %q, want dark", got)
	}
}

func TestCoerceTheme_EmptyDefaultsDark(t *testing.T) {
	if got := coerceTheme(""); got != "dark" {
		t.Errorf("got %q, want dark", got)
	}
}

// --- firstNonEmpty ---

func TestFirstNonEmpty_ReturnsFirst(t *testing.T) {
	if got := firstNonEmpty("a", "b", "c"); got != "a" {
		t.Errorf("got %q, want a", got)
	}
}

func TestFirstNonEmpty_SkipsEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", "c"); got != "c" {
		t.Errorf("got %q, want c", got)
	}
}

func TestFirstNonEmpty_AllEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  "); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// --- maxInt ---

func TestMaxInt(t *testing.T) {
	cases := [][3]int{{1, 2, 2}, {5, 3, 5}, {4, 4, 4}}
	for _, c := range cases {
		if got := maxInt(c[0], c[1]); got != c[2] {
			t.Errorf("maxInt(%d,%d)=%d, want %d", c[0], c[1], got, c[2])
		}
	}
}

// --- buildIndexDatabaseURL ---

func TestBuildIndexDatabaseURL_ExplicitURL(t *testing.T) {
	t.Setenv("INDEX_DATABASE_URL", "postgresql://user:pass@host/db")
	clearEnvKeys(t, "INDEX_DATABASE_USER", "INDEX_DATABASE_PASSWORD", "POSTGRES_USER", "POSTGRES_PASSWORD")
	got, err := buildIndexDatabaseURL()
	if err != nil || got != "postgresql://user:pass@host/db" {
		t.Errorf("got (%q, %v)", got, err)
	}
}

func TestBuildIndexDatabaseURL_Constructed(t *testing.T) {
	clearEnvKeys(t, "INDEX_DATABASE_URL")
	t.Setenv("INDEX_DATABASE_USER", "myuser")
	t.Setenv("INDEX_DATABASE_PASSWORD", "mypass")
	t.Setenv("INDEX_DATABASE_HOST", "db.host")
	t.Setenv("INDEX_DATABASE_PORT", "5433")
	t.Setenv("INDEX_DATABASE_NAME", "mydb")
	clearEnvKeys(t, "POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB")

	got, err := buildIndexDatabaseURL()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "db.host:5433/mydb") {
		t.Errorf("got %q, want host:port/db in URL", got)
	}
}

func TestBuildIndexDatabaseURL_FallbackPostgresCreds(t *testing.T) {
	clearEnvKeys(t, "INDEX_DATABASE_URL", "INDEX_DATABASE_USER", "INDEX_DATABASE_PASSWORD")
	t.Setenv("POSTGRES_USER", "pguser")
	t.Setenv("POSTGRES_PASSWORD", "pgpass")
	clearEnvKeys(t, "INDEX_DATABASE_HOST", "INDEX_DATABASE_PORT", "INDEX_DATABASE_NAME", "POSTGRES_DB")

	got, err := buildIndexDatabaseURL()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "pguser") {
		t.Errorf("expected pguser in URL, got %q", got)
	}
}

func TestBuildIndexDatabaseURL_MissingCreds(t *testing.T) {
	clearEnvKeys(t, "INDEX_DATABASE_URL", "INDEX_DATABASE_USER", "INDEX_DATABASE_PASSWORD", "POSTGRES_USER", "POSTGRES_PASSWORD")
	_, err := buildIndexDatabaseURL()
	if err == nil {
		t.Error("expected error when no credentials provided")
	}
}

func TestBuildIndexDatabaseURL_DefaultDBName(t *testing.T) {
	clearEnvKeys(t, "INDEX_DATABASE_URL", "INDEX_DATABASE_NAME", "POSTGRES_DB")
	t.Setenv("INDEX_DATABASE_USER", "u")
	t.Setenv("INDEX_DATABASE_PASSWORD", "p")
	clearEnvKeys(t, "INDEX_DATABASE_HOST", "INDEX_DATABASE_PORT")

	got, err := buildIndexDatabaseURL()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(got, "/three_to_one_go") {
		t.Errorf("expected default db name 'three_to_one_go', got %q", got)
	}
}

// --- path helpers ---

func TestDefaultConfigDir_ContainerLayout(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/config")
	got := DefaultConfigDir()
	if got != "/config" {
		t.Errorf("got %q, want /config", got)
	}
}

func TestDefaultConfigDir_CustomXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
	got := DefaultConfigDir()
	if !strings.HasSuffix(got, AppDirName) || !strings.HasPrefix(got, "/custom/xdg") {
		t.Errorf("got %q", got)
	}
}

func TestDefaultConfigDir_HomeDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	got := DefaultConfigDir()
	if !strings.HasSuffix(got, AppDirName) {
		t.Errorf("got %q, expected to end with %s", got, AppDirName)
	}
}

func TestHookScriptsDir_ContainerLayout(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/config")
	got := HookScriptsDir()
	if got != "/hook-scripts" {
		t.Errorf("got %q, want /hook-scripts", got)
	}
}

func TestHookScriptsDir_NonContainer(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	got := HookScriptsDir()
	if !strings.HasSuffix(got, "hook-scripts") {
		t.Errorf("got %q", got)
	}
}

func TestTrustedCertificatesDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	got := TrustedCertificatesDir()
	if !strings.HasSuffix(got, "trusted-certs") {
		t.Errorf("got %q, want suffix trusted-certs", got)
	}
}

func TestIssuerKeyPathFromEnv_EnvSet(t *testing.T) {
	t.Setenv("ISSUER_KEY_FILE", "/tmp/my.key")
	if got := IssuerKeyPathFromEnv(); got != "/tmp/my.key" {
		t.Errorf("got %q, want /tmp/my.key", got)
	}
}

func TestIssuerKeyPathFromEnv_Default(t *testing.T) {
	t.Setenv("ISSUER_KEY_FILE", "")
	got := IssuerKeyPathFromEnv()
	if !strings.HasSuffix(got, "issuer.key") {
		t.Errorf("got %q, expected issuer.key suffix", got)
	}
}

// --- Settings helpers ---

func TestMaxUploadSizeBytes(t *testing.T) {
	s := &Settings{MaxUploadSizeMB: 10}
	want := int64(10 * 1024 * 1024)
	if got := s.MaxUploadSizeBytes(); got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestUploadChunkSizeBytes(t *testing.T) {
	s := &Settings{UploadChunkSizeMB: 8}
	want := int64(8 * 1024 * 1024)
	if got := s.UploadChunkSizeBytes(); got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestSettingsToPayload_RoundTrip(t *testing.T) {
	s := &Settings{
		RetentionKeepLast:      5,
		LogLevel:               "DEBUG",
		Theme:                  "light",
		MaxUploadSizeMB:        1024,
		UploadChunkSizeMB:      16,
		UploadSessionTTLHours:  48,
		UploadCleanupIntervalS: 600,
		NtfyURL:                "https://ntfy.sh",
		NtfyTopic:              "mytopic",
		NtfyMessageTemplate:    "hello",
		NtfyMatchEdgeID:        "edge1",
		NtfyMatchEdgeInstID:    "inst1",
		NtfyMatchSource:        "1.2.3.4",
		HookPreCommand:         "pre.sh",
		HookPostCommand:        "post.sh",
	}
	p := SettingsToPayload(s)
	if p.RetentionKeepLast != 5 || p.LogLevel != "DEBUG" || p.Theme != "light" {
		t.Errorf("payload mismatch: %+v", p)
	}
	if p.NtfyURL != "https://ntfy.sh" || p.HookPreCommand != "pre.sh" {
		t.Errorf("payload mismatch: %+v", p)
	}
}

// --- BuildSettings ---

func TestBuildSettings_Defaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ISSUER_KEY_FILE", filepath.Join(dir, "issuer.key"))
	t.Setenv("INDEX_DATABASE_URL", "postgresql://u:p@h/d")
	clearEnvKeys(t, "HTTP_PORT", "HTTP_HOST", "STORAGE_BACKEND", "BACKUP_ROOT", "STAGING_DIR", "XDG_CONFIG_HOME")

	s, err := BuildSettings(nil)
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	if s.HTTPPort != 6555 {
		t.Errorf("HTTPPort = %d, want 6555", s.HTTPPort)
	}
	if s.StorageBackend != "local" {
		t.Errorf("StorageBackend = %q", s.StorageBackend)
	}
	if s.RetentionKeepLast != 3 {
		t.Errorf("RetentionKeepLast = %d", s.RetentionKeepLast)
	}
	if s.Theme != "dark" {
		t.Errorf("Theme = %q", s.Theme)
	}
}

func TestBuildSettings_WithPayload(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ISSUER_KEY_FILE", filepath.Join(dir, "issuer.key"))
	t.Setenv("INDEX_DATABASE_URL", "postgresql://u:p@h/d")

	p := &SettingsPayload{
		RetentionKeepLast: 10,
		LogLevel:          "warn",
		Theme:             "light",
		MaxUploadSizeMB:   512,
		NtfyURL:           "https://ntfy.example.com",
		NtfyTopic:         "alerts",
	}
	s, err := BuildSettings(p)
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	if s.RetentionKeepLast != 10 {
		t.Errorf("RetentionKeepLast = %d", s.RetentionKeepLast)
	}
	if s.LogLevel != "WARN" {
		t.Errorf("LogLevel = %q", s.LogLevel)
	}
	if s.Theme != "light" {
		t.Errorf("Theme = %q", s.Theme)
	}
	if s.NtfyURL != "https://ntfy.example.com" {
		t.Errorf("NtfyURL = %q", s.NtfyURL)
	}
}

func TestBuildSettings_BadNtfyURL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ISSUER_KEY_FILE", filepath.Join(dir, "issuer.key"))
	t.Setenv("INDEX_DATABASE_URL", "postgresql://u:p@h/d")

	p := &SettingsPayload{NtfyURL: "ftp://bad.url"}
	_, err := BuildSettings(p)
	if err == nil {
		t.Error("expected error for bad ntfy URL")
	}
}

func TestBuildSettings_HTTPPortEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ISSUER_KEY_FILE", filepath.Join(dir, "issuer.key"))
	t.Setenv("INDEX_DATABASE_URL", "postgresql://u:p@h/d")
	t.Setenv("HTTP_PORT", "8080")

	s, err := BuildSettings(nil)
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	if s.HTTPPort != 8080 {
		t.Errorf("HTTPPort = %d, want 8080", s.HTTPPort)
	}
}

func TestBuildSettings_MissingDBCreds(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ISSUER_KEY_FILE", filepath.Join(dir, "issuer.key"))
	clearEnvKeys(t, "INDEX_DATABASE_URL", "INDEX_DATABASE_USER", "INDEX_DATABASE_PASSWORD", "POSTGRES_USER", "POSTGRES_PASSWORD")

	_, err := BuildSettings(nil)
	if err == nil {
		t.Error("expected error for missing DB credentials")
	}
}

func TestBuildSettings_PayloadMinimums(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ISSUER_KEY_FILE", filepath.Join(dir, "issuer.key"))
	t.Setenv("INDEX_DATABASE_URL", "postgresql://u:p@h/d")

	p := &SettingsPayload{
		RetentionKeepLast:      0, // zero means use default
		MaxUploadSizeMB:        0,
		UploadChunkSizeMB:      0,
		UploadSessionTTLHours:  0,
		UploadCleanupIntervalS: 0,
	}
	s, err := BuildSettings(p)
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	// All zeroes should use defaults
	if s.RetentionKeepLast != 3 {
		t.Errorf("RetentionKeepLast = %d, want 3", s.RetentionKeepLast)
	}
	if s.MaxUploadSizeMB != 2048 {
		t.Errorf("MaxUploadSizeMB = %d, want 2048", s.MaxUploadSizeMB)
	}
}

func init() {
	// Ensure tests don't accidentally touch real env state
	_ = os.Getenv
}
