package config

import (
	"testing"
)

// --- coerceInt ---

func TestCoerceInt_ZeroReturnsDefault(t *testing.T) {
	if got := coerceInt(0, 42, 0); got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestCoerceInt_PositiveValue(t *testing.T) {
	if got := coerceInt(10, 0, 0); got != 10 {
		t.Errorf("got %d, want 10", got)
	}
}

func TestCoerceInt_BelowMinClamped(t *testing.T) {
	if got := coerceInt(2, 10, 5); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
}

func TestCoerceInt_NegativeValueClamped(t *testing.T) {
	if got := coerceInt(-1, 10, 0); got != 0 {
		t.Errorf("got %d, want 0 (clamped to min)", got)
	}
}

func TestCoerceInt_ExactlyMin(t *testing.T) {
	if got := coerceInt(5, 10, 5); got != 5 {
		t.Errorf("got %d, want 5", got)
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

func TestCoerceText_NonEmptyReturned(t *testing.T) {
	if got := coerceText("value", "default"); got != "value" {
		t.Errorf("got %q, want value", got)
	}
}

// --- coerceURL ---

func TestCoerceURL_EmptyUsesDefault(t *testing.T) {
	v, err := coerceURL("", "http://127.0.0.1:6555")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "http://127.0.0.1:6555" {
		t.Errorf("got %q, want http://127.0.0.1:6555", v)
	}
}

func TestCoerceURL_BothEmptyReturnsEmpty(t *testing.T) {
	v, err := coerceURL("", "")
	if err != nil || v != "" {
		t.Errorf("got (%q, %v), want ('', nil)", v, err)
	}
}

func TestCoerceURL_ValidHTTP(t *testing.T) {
	v, err := coerceURL("http://central.example.com:6555", "")
	if err != nil || v != "http://central.example.com:6555" {
		t.Errorf("got (%q, %v)", v, err)
	}
}

func TestCoerceURL_ValidHTTPS(t *testing.T) {
	v, err := coerceURL("https://central.example.com", "")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if v != "https://central.example.com" {
		t.Errorf("got %q", v)
	}
}

func TestCoerceURL_TrailingSlashStripped(t *testing.T) {
	v, err := coerceURL("https://example.com/", "")
	if err != nil || v != "https://example.com" {
		t.Errorf("got (%q, %v)", v, err)
	}
}

func TestCoerceURL_InvalidScheme(t *testing.T) {
	if _, err := coerceURL("ftp://example.com", ""); err == nil {
		t.Error("expected error for ftp scheme")
	}
}

func TestCoerceURL_NotAURL(t *testing.T) {
	if _, err := coerceURL("not-a-url", ""); err == nil {
		t.Error("expected error for non-URL string")
	}
}

func TestCoerceURL_NoHost(t *testing.T) {
	if _, err := coerceURL("http://", ""); err == nil {
		t.Error("expected error for URL with no host")
	}
}

// --- coerceTheme ---

func TestCoerceTheme_Light(t *testing.T) {
	if got := coerceTheme("light"); got != "light" {
		t.Errorf("got %q, want light", got)
	}
}

func TestCoerceTheme_LightCaseInsensitive(t *testing.T) {
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

// --- coerceBoolPtr ---

func TestCoerceBoolPtr_NilReturnsDefault(t *testing.T) {
	if got := coerceBoolPtr(nil, true); !got {
		t.Error("nil pointer should return default true")
	}
	if got := coerceBoolPtr(nil, false); got {
		t.Error("nil pointer should return default false")
	}
}

func TestCoerceBoolPtr_ExplicitFalseIgnoresDefault(t *testing.T) {
	b := false
	if got := coerceBoolPtr(&b, true); got {
		t.Error("explicit false should not be overridden by default true")
	}
}

func TestCoerceBoolPtr_ExplicitTrue(t *testing.T) {
	b := true
	if got := coerceBoolPtr(&b, false); !got {
		t.Error("explicit true should be returned")
	}
}

// --- parseBoolEnv ---

func TestParseBoolEnv_TrueValues(t *testing.T) {
	for _, v := range []string{"1", "true", "yes", "on", "TRUE", "YES", "ON"} {
		if !parseBoolEnv(v, false) {
			t.Errorf("expected true for %q", v)
		}
	}
}

func TestParseBoolEnv_FalseValues(t *testing.T) {
	for _, v := range []string{"0", "false", "no", "off", "FALSE", "NO"} {
		if parseBoolEnv(v, true) {
			t.Errorf("expected false for %q", v)
		}
	}
}

func TestParseBoolEnv_UnknownUsesDefault(t *testing.T) {
	if !parseBoolEnv("maybe", true) {
		t.Error("expected default true for unknown value")
	}
	if parseBoolEnv("maybe", false) {
		t.Error("expected default false for unknown value")
	}
}

// --- UploadChunkSizeBytes helpers ---

func TestUploadChunkSizeBytes(t *testing.T) {
	s := &Settings{UploadChunkSizeMB: 8}
	if got := s.UploadChunkSizeBytes(); got != 8*1024*1024 {
		t.Errorf("got %d, want %d", got, 8*1024*1024)
	}
}

func TestMinUploadChunkSizeBytes(t *testing.T) {
	s := &Settings{MinUploadChunkSizeMB: 1}
	if got := s.MinUploadChunkSizeBytes(); got != 1*1024*1024 {
		t.Errorf("got %d, want %d", got, 1024*1024)
	}
}

func TestMaxUploadChunkSizeBytes(t *testing.T) {
	s := &Settings{MaxUploadChunkSizeMB: 16}
	if got := s.MaxUploadChunkSizeBytes(); got != 16*1024*1024 {
		t.Errorf("got %d, want %d", got, 16*1024*1024)
	}
}

// --- SettingsToPayload ---

func TestSettingsToPayload_RoundTrip(t *testing.T) {
	s := &Settings{
		EdgeID:       "my-edge",
		CronSchedule: "0 2 * * 0",
		LogLevel:     "DEBUG",
		Theme:        "light",
		HTTPPort:     6556,
		NtfyURL:      "https://ntfy.sh",
		NtfyTopic:    "alerts",
	}
	p := SettingsToPayload(s)
	if p.EdgeID != "my-edge" {
		t.Errorf("EdgeID = %q, want my-edge", p.EdgeID)
	}
	if p.LogLevel != "DEBUG" {
		t.Errorf("LogLevel = %q, want DEBUG", p.LogLevel)
	}
	if p.HTTPPort != 6556 {
		t.Errorf("HTTPPort = %d, want 6556", p.HTTPPort)
	}
	if p.NtfyURL != "https://ntfy.sh" {
		t.Errorf("NtfyURL = %q, want https://ntfy.sh", p.NtfyURL)
	}
}

// --- BuildSettings ---

func TestBuildSettings_Defaults(t *testing.T) {
	t.Setenv("EDGE_ID", "")
	t.Setenv("HTTP_PORT", "")
	t.Setenv("CENTRAL_URL", "")
	t.Setenv("LOG_LEVEL", "")

	s, err := BuildSettings(nil)
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	if s.HTTPPort != 6556 {
		t.Errorf("HTTPPort = %d, want 6556", s.HTTPPort)
	}
	if s.LogLevel != "INFO" {
		t.Errorf("LogLevel = %q, want INFO", s.LogLevel)
	}
	if s.Theme != "dark" {
		t.Errorf("Theme = %q, want dark", s.Theme)
	}
	if s.EdgeID != "edge-01" {
		t.Errorf("EdgeID = %q, want edge-01", s.EdgeID)
	}
	if s.UploadChunkSizeMB != 8 {
		t.Errorf("UploadChunkSizeMB = %d, want 8", s.UploadChunkSizeMB)
	}
}

func TestBuildSettings_EnvOverridePort(t *testing.T) {
	t.Setenv("HTTP_PORT", "8080")
	t.Setenv("CENTRAL_URL", "")

	s, err := BuildSettings(nil)
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	if s.HTTPPort != 8080 {
		t.Errorf("HTTPPort = %d, want 8080", s.HTTPPort)
	}
}

func TestBuildSettings_EnvOverrideEdgeID(t *testing.T) {
	t.Setenv("EDGE_ID", "my-edge")
	t.Setenv("HTTP_PORT", "")
	t.Setenv("CENTRAL_URL", "")

	s, err := BuildSettings(nil)
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	if s.EdgeID != "my-edge" {
		t.Errorf("EdgeID = %q, want my-edge", s.EdgeID)
	}
}

func TestBuildSettings_PayloadOverride(t *testing.T) {
	t.Setenv("EDGE_ID", "")
	t.Setenv("HTTP_PORT", "")
	t.Setenv("CENTRAL_URL", "")

	p := &SettingsPayload{
		EdgeID:   "from-payload",
		LogLevel: "warn",
		Theme:    "light",
		HTTPPort: 9090,
	}
	s, err := BuildSettings(p)
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	if s.EdgeID != "from-payload" {
		t.Errorf("EdgeID = %q, want from-payload", s.EdgeID)
	}
	if s.LogLevel != "WARN" {
		t.Errorf("LogLevel = %q, want WARN", s.LogLevel)
	}
	if s.Theme != "light" {
		t.Errorf("Theme = %q, want light", s.Theme)
	}
	if s.HTTPPort != 9090 {
		t.Errorf("HTTPPort = %d, want 9090", s.HTTPPort)
	}
}

func TestBuildSettings_EnvTakesPrecedenceOverPayload(t *testing.T) {
	t.Setenv("EDGE_ID", "from-env")
	t.Setenv("HTTP_PORT", "")
	t.Setenv("CENTRAL_URL", "")

	p := &SettingsPayload{EdgeID: "from-payload"}
	s, err := BuildSettings(p)
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	if s.EdgeID != "from-env" {
		t.Errorf("EdgeID = %q, want from-env (env should win)", s.EdgeID)
	}
}

func TestBuildSettings_KeepLocalPendingDefaultsTrue(t *testing.T) {
	t.Setenv("EDGE_ID", "")
	t.Setenv("HTTP_PORT", "")
	t.Setenv("CENTRAL_URL", "")
	t.Setenv("KEEP_LOCAL_PENDING", "")

	s, err := BuildSettings(nil)
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	if !s.KeepLocalPending {
		t.Error("KeepLocalPending should default to true on a fresh install")
	}
}

func TestBuildSettings_KeepLocalPendingExplicitFalse(t *testing.T) {
	t.Setenv("EDGE_ID", "")
	t.Setenv("HTTP_PORT", "")
	t.Setenv("CENTRAL_URL", "")
	t.Setenv("KEEP_LOCAL_PENDING", "")

	b := false
	p := &SettingsPayload{KeepLocalPending: &b}
	s, err := BuildSettings(p)
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	if s.KeepLocalPending {
		t.Error("KeepLocalPending should be false when explicitly set to false in payload")
	}
}

func TestBuildSettings_BadCentralURL(t *testing.T) {
	t.Setenv("CENTRAL_URL", "ftp://bad.url")
	defer t.Setenv("CENTRAL_URL", "")
	if _, err := BuildSettings(nil); err == nil {
		t.Error("expected error for bad central URL")
	}
}

func TestBuildSettings_BadNtfyURL(t *testing.T) {
	t.Setenv("CENTRAL_URL", "")
	p := &SettingsPayload{NtfyURL: "not-a-url"}
	if _, err := BuildSettings(p); err == nil {
		t.Error("expected error for bad ntfy URL")
	}
}

func TestBuildSettings_LogLevelUppercased(t *testing.T) {
	t.Setenv("EDGE_ID", "")
	t.Setenv("HTTP_PORT", "")
	t.Setenv("CENTRAL_URL", "")

	p := &SettingsPayload{LogLevel: "debug"}
	s, err := BuildSettings(p)
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	if s.LogLevel != "DEBUG" {
		t.Errorf("LogLevel = %q, want DEBUG (should be uppercased)", s.LogLevel)
	}
}

func TestBuildSettings_MinimumEnforced(t *testing.T) {
	t.Setenv("EDGE_ID", "")
	t.Setenv("HTTP_PORT", "0") // 0 triggers default, not below-min
	t.Setenv("CENTRAL_URL", "")

	s, err := BuildSettings(nil)
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	// Port 0 from env is ignored (Atoi gives 0, which coerceInt returns default for)
	if s.HTTPPort != 6556 {
		t.Errorf("HTTPPort = %d, want 6556 (0 should use default)", s.HTTPPort)
	}
}
