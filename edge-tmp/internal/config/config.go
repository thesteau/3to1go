package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const AppDirName = "3to1goEdge"

// Settings holds all runtime configuration for the edge server.
type Settings struct {
	EdgeID                            string
	ScanRoot                          string
	CentralURL                        string
	AdvertisedURL                     string
	EdgeCredential                    string
	CronSchedule                      string
	StateDir                          string
	SpoolDir                          string
	LogLevel                          string
	Theme                             string
	MaxDepth                          int
	KeepLocalPending                  bool
	UploadChunkSizeMB                 int
	MinUploadChunkSizeMB              int
	MaxUploadChunkSizeMB              int
	UploadRetryMaxAttempts            int
	UploadRetryBaseDelaySeconds       int
	UploadRetryMaxDelaySeconds        int
	UploadConnectTimeoutSeconds       int
	UploadReadTimeoutPaddingSeconds   int
	UploadMinThroughputBytesPerSecond int
	CircuitBreakerFailureThreshold    int
	CircuitBreakerCooldownSeconds     int
	NtfyURL                           string
	NtfyTopic                         string
	NtfyMessageTemplate               string
	HookPreCommand                    string
	HookPostCommand                   string
	HTTPHost                          string
	HTTPPort                          int
}

func (s *Settings) UploadChunkSizeBytes() int64 {
	return int64(s.UploadChunkSizeMB) * 1024 * 1024
}

func (s *Settings) MinUploadChunkSizeBytes() int64 {
	return int64(s.MinUploadChunkSizeMB) * 1024 * 1024
}

func (s *Settings) MaxUploadChunkSizeBytes() int64 {
	return int64(s.MaxUploadChunkSizeMB) * 1024 * 1024
}

func usesContainerLayout() bool {
	return strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")) == "/config"
}

func DefaultConfigDir() string {
	if usesContainerLayout() {
		return "/config"
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", AppDirName)
	}
	xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if xdg != "" {
		return filepath.Join(xdg, AppDirName)
	}
	return filepath.Join(home, ".config", AppDirName)
}

func DefaultStateDir() string {
	if strings.TrimSpace(os.Getenv("XDG_STATE_HOME")) == "/data/state" {
		return "/data/state"
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(DefaultConfigDir(), "state")
	}
	xdg := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	if xdg != "" {
		return filepath.Join(xdg, AppDirName)
	}
	return filepath.Join(home, ".local", "state", AppDirName)
}

func DefaultSpoolDir() string {
	if strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")) == "/data/cache" {
		return "/data/spool"
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Caches", AppDirName, "spool")
	}
	xdg := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME"))
	if xdg != "" {
		return filepath.Join(xdg, AppDirName, "spool")
	}
	return filepath.Join(home, ".cache", AppDirName, "spool")
}

func DefaultScanRoot() string {
	if usesContainerLayout() {
		return "/scan"
	}
	if runtime.GOOS == "windows" {
		return `C:\Users`
	}
	if runtime.GOOS == "darwin" {
		return "/Users"
	}
	return "/home"
}

func DefaultHTTPHost() string {
	if usesContainerLayout() {
		return "0.0.0.0"
	}
	return "127.0.0.1"
}

func HookScriptsDir() string {
	if usesContainerLayout() {
		return "/hook-scripts"
	}
	return filepath.Join(DefaultConfigDir(), "hook-scripts")
}

func TrustedCertificatesDir() string {
	return filepath.Join(DefaultConfigDir(), "trusted-certs")
}

func EncryptionKeyPath() string {
	return filepath.Join(DefaultConfigDir(), "encryption.key")
}

func InstallationIDPath() string {
	return filepath.Join(DefaultConfigDir(), "installation.id")
}

func AppDatabasePath() string {
	return filepath.Join(DefaultConfigDir(), "3to1go-edge.db")
}

// SettingsPayload is the serializable subset stored in the database.
type SettingsPayload struct {
	EdgeID                            string `json:"edge_id"`
	ScanRoot                          string `json:"scan_root"`
	CentralURL                        string `json:"central_url"`
	AdvertisedURL                     string `json:"advertised_url"`
	EdgeCredential                    string `json:"edge_credential"`
	CronSchedule                      string `json:"cron_schedule"`
	StateDir                          string `json:"state_dir"`
	SpoolDir                          string `json:"spool_dir"`
	LogLevel                          string `json:"log_level"`
	Theme                             string `json:"theme"`
	MaxDepth                          int    `json:"max_depth"`
	KeepLocalPending                  bool   `json:"keep_local_pending"`
	UploadChunkSizeMB                 int    `json:"upload_chunk_size_mb"`
	MinUploadChunkSizeMB              int    `json:"min_upload_chunk_size_mb"`
	MaxUploadChunkSizeMB              int    `json:"max_upload_chunk_size_mb"`
	UploadRetryMaxAttempts            int    `json:"upload_retry_max_attempts"`
	UploadRetryBaseDelaySeconds       int    `json:"upload_retry_base_delay_seconds"`
	UploadRetryMaxDelaySeconds        int    `json:"upload_retry_max_delay_seconds"`
	UploadConnectTimeoutSeconds       int    `json:"upload_connect_timeout_seconds"`
	UploadReadTimeoutPaddingSeconds   int    `json:"upload_read_timeout_padding_seconds"`
	UploadMinThroughputBytesPerSecond int    `json:"upload_min_throughput_bytes_per_second"`
	CircuitBreakerFailureThreshold    int    `json:"circuit_breaker_failure_threshold"`
	CircuitBreakerCooldownSeconds     int    `json:"circuit_breaker_cooldown_seconds"`
	NtfyURL                           string `json:"ntfy_url"`
	NtfyTopic                         string `json:"ntfy_topic"`
	NtfyMessageTemplate               string `json:"ntfy_message_template"`
	HookPreCommand                    string `json:"hook_pre_command"`
	HookPostCommand                   string `json:"hook_post_command"`
	HTTPHost                          string `json:"http_host"`
	HTTPPort                          int    `json:"http_port"`
}

func SettingsToPayload(s *Settings) SettingsPayload {
	return SettingsPayload{
		EdgeID:                            s.EdgeID,
		ScanRoot:                          s.ScanRoot,
		CentralURL:                        s.CentralURL,
		AdvertisedURL:                     s.AdvertisedURL,
		EdgeCredential:                    s.EdgeCredential,
		CronSchedule:                      s.CronSchedule,
		StateDir:                          s.StateDir,
		SpoolDir:                          s.SpoolDir,
		LogLevel:                          s.LogLevel,
		Theme:                             s.Theme,
		MaxDepth:                          s.MaxDepth,
		KeepLocalPending:                  s.KeepLocalPending,
		UploadChunkSizeMB:                 s.UploadChunkSizeMB,
		MinUploadChunkSizeMB:              s.MinUploadChunkSizeMB,
		MaxUploadChunkSizeMB:              s.MaxUploadChunkSizeMB,
		UploadRetryMaxAttempts:            s.UploadRetryMaxAttempts,
		UploadRetryBaseDelaySeconds:       s.UploadRetryBaseDelaySeconds,
		UploadRetryMaxDelaySeconds:        s.UploadRetryMaxDelaySeconds,
		UploadConnectTimeoutSeconds:       s.UploadConnectTimeoutSeconds,
		UploadReadTimeoutPaddingSeconds:   s.UploadReadTimeoutPaddingSeconds,
		UploadMinThroughputBytesPerSecond: s.UploadMinThroughputBytesPerSecond,
		CircuitBreakerFailureThreshold:    s.CircuitBreakerFailureThreshold,
		CircuitBreakerCooldownSeconds:     s.CircuitBreakerCooldownSeconds,
		NtfyURL:                           s.NtfyURL,
		NtfyTopic:                         s.NtfyTopic,
		NtfyMessageTemplate:               s.NtfyMessageTemplate,
		HookPreCommand:                    s.HookPreCommand,
		HookPostCommand:                   s.HookPostCommand,
		HTTPHost:                          s.HTTPHost,
		HTTPPort:                          s.HTTPPort,
	}
}

// BuildSettings constructs Settings from a SettingsPayload merged with environment.
// If p is nil, all values fall back to defaults and environment variables.
func BuildSettings(p *SettingsPayload) (*Settings, error) {
	raw := &SettingsPayload{}
	if p != nil {
		*raw = *p
	}

	// Apply env overrides (env takes precedence over DB payload)
	applyEnvOverrides(raw)

	cronExpr := coerceText(raw.CronSchedule, "0 2 * * 0")
	if err := validateCronExpression(cronExpr); err != nil {
		return nil, fmt.Errorf("cron_schedule: %w", err)
	}

	centralURL, err := coerceURL(raw.CentralURL, "http://127.0.0.1:6555")
	if err != nil {
		return nil, fmt.Errorf("central_url: %w", err)
	}
	advertisedURL, err := coerceURL(raw.AdvertisedURL, "")
	if err != nil {
		return nil, fmt.Errorf("advertised_url: %w", err)
	}
	ntfyURL, err := coerceURL(raw.NtfyURL, "")
	if err != nil {
		return nil, fmt.Errorf("ntfy_url: %w", err)
	}

	stateDir := coerceText(raw.StateDir, DefaultStateDir())
	spoolDir := coerceText(raw.SpoolDir, DefaultSpoolDir())
	httpHost := coerceText(raw.HTTPHost, DefaultHTTPHost())
	httpPort := coerceInt(raw.HTTPPort, 6556, 1)

	return &Settings{
		EdgeID:                            coerceText(raw.EdgeID, "edge-01"),
		ScanRoot:                          coerceText(raw.ScanRoot, DefaultScanRoot()),
		CentralURL:                        centralURL,
		AdvertisedURL:                     advertisedURL,
		EdgeCredential:                    strings.TrimSpace(raw.EdgeCredential),
		CronSchedule:                      cronExpr,
		StateDir:                          stateDir,
		SpoolDir:                          spoolDir,
		LogLevel:                          strings.ToUpper(coerceText(raw.LogLevel, "INFO")),
		Theme:                             coerceTheme(raw.Theme),
		MaxDepth:                          coerceInt(raw.MaxDepth, 10, 0),
		KeepLocalPending:                  coerceBool(raw.KeepLocalPending, true),
		UploadChunkSizeMB:                 coerceInt(raw.UploadChunkSizeMB, 8, 1),
		MinUploadChunkSizeMB:              coerceInt(raw.MinUploadChunkSizeMB, 1, 1),
		MaxUploadChunkSizeMB:              coerceInt(raw.MaxUploadChunkSizeMB, 16, 1),
		UploadRetryMaxAttempts:            coerceInt(raw.UploadRetryMaxAttempts, 5, 1),
		UploadRetryBaseDelaySeconds:       coerceInt(raw.UploadRetryBaseDelaySeconds, 5, 1),
		UploadRetryMaxDelaySeconds:        coerceInt(raw.UploadRetryMaxDelaySeconds, 300, 1),
		UploadConnectTimeoutSeconds:       coerceInt(raw.UploadConnectTimeoutSeconds, 10, 1),
		UploadReadTimeoutPaddingSeconds:   coerceInt(raw.UploadReadTimeoutPaddingSeconds, 30, 5),
		UploadMinThroughputBytesPerSecond: coerceInt(raw.UploadMinThroughputBytesPerSecond, 262144, 1024),
		CircuitBreakerFailureThreshold:    coerceInt(raw.CircuitBreakerFailureThreshold, 5, 1),
		CircuitBreakerCooldownSeconds:     coerceInt(raw.CircuitBreakerCooldownSeconds, 300, 1),
		NtfyURL:                           ntfyURL,
		NtfyTopic:                         strings.TrimSpace(raw.NtfyTopic),
		NtfyMessageTemplate:               strings.TrimSpace(raw.NtfyMessageTemplate),
		HookPreCommand:                    strings.TrimSpace(raw.HookPreCommand),
		HookPostCommand:                   strings.TrimSpace(raw.HookPostCommand),
		HTTPHost:                          httpHost,
		HTTPPort:                          httpPort,
	}, nil
}

func applyEnvOverrides(p *SettingsPayload) {
	if v := os.Getenv("EDGE_ID"); v != "" {
		p.EdgeID = v
	}
	if v := os.Getenv("SCAN_ROOT"); v != "" {
		p.ScanRoot = v
	}
	if v := os.Getenv("CENTRAL_URL"); v != "" {
		p.CentralURL = v
	}
	if v := os.Getenv("STATE_DIR"); v != "" {
		p.StateDir = v
	}
	if v := os.Getenv("SPOOL_DIR"); v != "" {
		p.SpoolDir = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		p.LogLevel = v
	}
	if v := os.Getenv("MAX_DEPTH"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			p.MaxDepth = n
		}
	}
	if v := os.Getenv("KEEP_LOCAL_PENDING"); v != "" {
		p.KeepLocalPending = parseBoolEnv(v, p.KeepLocalPending)
	}
	if v := os.Getenv("UPLOAD_CHUNK_SIZE_MB"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			p.UploadChunkSizeMB = n
		}
	}
	if v := os.Getenv("MIN_UPLOAD_CHUNK_SIZE_MB"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			p.MinUploadChunkSizeMB = n
		}
	}
	if v := os.Getenv("MAX_UPLOAD_CHUNK_SIZE_MB"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			p.MaxUploadChunkSizeMB = n
		}
	}
	if v := os.Getenv("UPLOAD_RETRY_MAX_ATTEMPTS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			p.UploadRetryMaxAttempts = n
		}
	}
	if v := os.Getenv("UPLOAD_RETRY_BASE_DELAY_SECONDS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			p.UploadRetryBaseDelaySeconds = n
		}
	}
	if v := os.Getenv("UPLOAD_RETRY_MAX_DELAY_SECONDS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			p.UploadRetryMaxDelaySeconds = n
		}
	}
	if v := os.Getenv("UPLOAD_CONNECT_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			p.UploadConnectTimeoutSeconds = n
		}
	}
	if v := os.Getenv("UPLOAD_READ_TIMEOUT_PADDING_SECONDS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			p.UploadReadTimeoutPaddingSeconds = n
		}
	}
	if v := os.Getenv("UPLOAD_MIN_THROUGHPUT_BYTES_PER_SECOND"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			p.UploadMinThroughputBytesPerSecond = n
		}
	}
	if v := os.Getenv("CIRCUIT_BREAKER_FAILURE_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			p.CircuitBreakerFailureThreshold = n
		}
	}
	if v := os.Getenv("CIRCUIT_BREAKER_COOLDOWN_SECONDS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			p.CircuitBreakerCooldownSeconds = n
		}
	}
	if v := os.Getenv("HTTP_HOST"); v != "" {
		p.HTTPHost = v
	}
	if v := os.Getenv("HTTP_PORT"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			p.HTTPPort = n
		}
	}
}

func coerceInt(value, def, min int) int {
	if value == 0 {
		return def
	}
	if value < min {
		return min
	}
	return value
}

func coerceText(value, def string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return def
	}
	return v
}

func coerceURL(value, def string) (string, error) {
	v := strings.TrimRight(strings.TrimSpace(value), "/")
	if v == "" {
		v = def
	}
	if v == "" {
		return "", nil
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("url must be a full http or https URL")
	}
	return v, nil
}

func coerceTheme(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "light" {
		return "light"
	}
	return "dark"
}

func coerceBool(value bool, def bool) bool {
	return value
}

func parseBoolEnv(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

// validateCronExpression is a lightweight check delegated to schedule package
// via a registered validator. We store the function here to avoid import cycles.
var validateCronExpression func(expr string) error = func(expr string) error { return nil }

// RegisterCronValidator allows the schedule package to register its validator.
func RegisterCronValidator(fn func(string) error) {
	validateCronExpression = fn
}
