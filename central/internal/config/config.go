package config

import (
	"crypto/ed25519"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/3to1go/central/internal/signing"
)

const AppDirName = "3to1goCentral"

// Settings holds all runtime configuration for the central server.
type Settings struct {
	IssuerKeyPath          string
	IssuerPublicKey        ed25519.PublicKey
	StorageBackend         string
	BackupRoot             string
	StagingDir             string
	RetentionKeepLast      int
	LogLevel               string
	MaxUploadSizeMB        int
	UploadChunkSizeMB      int
	UploadSessionTTLHours  int
	UploadCleanupIntervalS int
	NtfyURL                string
	NtfyTopic              string
	NtfyMessageTemplate    string
	NtfyMatchEdgeID        string
	NtfyMatchEdgeInstID    string
	NtfyMatchSource        string
	HookPreCommand         string
	HookPostCommand        string
	Theme                  string
	IndexDatabaseURL       string
	HTTPHost               string
	HTTPPort               int
}

func (s *Settings) MaxUploadSizeBytes() int64 {
	return int64(s.MaxUploadSizeMB) * 1024 * 1024
}

func (s *Settings) UploadChunkSizeBytes() int64 {
	return int64(s.UploadChunkSizeMB) * 1024 * 1024
}

func usesContainerLayout() bool {
	return strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")) == "/config"
}

func DefaultConfigDir() string {
	if usesContainerLayout() {
		return "/config"
	}
	home, _ := os.UserHomeDir()
	xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if xdg != "" {
		return filepath.Join(xdg, AppDirName)
	}
	return filepath.Join(home, ".config", AppDirName)
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

func IssuerKeyPathFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("ISSUER_KEY_FILE")); v != "" {
		return v
	}
	return filepath.Join(DefaultConfigDir(), "issuer.key")
}

func coerceInt(value string, def, min int) int {
	if value == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	return n
}

func coerceText(value, def string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return def
	}
	return v
}

func coerceURL(value string) (string, error) {
	v := strings.TrimRight(strings.TrimSpace(value), "/")
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

// SettingsPayload is the serializable subset stored in the database.
type SettingsPayload struct {
	RetentionKeepLast      int    `json:"retention_keep_last"`
	LogLevel               string `json:"log_level"`
	Theme                  string `json:"theme"`
	MaxUploadSizeMB        int    `json:"max_upload_size_mb"`
	UploadChunkSizeMB      int    `json:"upload_chunk_size_mb"`
	UploadSessionTTLHours  int    `json:"upload_session_ttl_hours"`
	UploadCleanupIntervalS int    `json:"upload_cleanup_interval_seconds"`
	NtfyURL                string `json:"ntfy_url"`
	NtfyTopic              string `json:"ntfy_topic"`
	NtfyMessageTemplate    string `json:"ntfy_message_template"`
	NtfyMatchEdgeID        string `json:"ntfy_match_edge_id"`
	NtfyMatchEdgeInstID    string `json:"ntfy_match_edge_instance_id"`
	NtfyMatchSource        string `json:"ntfy_match_source"`
	HookPreCommand         string `json:"hook_pre_command"`
	HookPostCommand        string `json:"hook_post_command"`
}

func SettingsToPayload(s *Settings) SettingsPayload {
	return SettingsPayload{
		RetentionKeepLast:      s.RetentionKeepLast,
		LogLevel:               s.LogLevel,
		Theme:                  s.Theme,
		MaxUploadSizeMB:        s.MaxUploadSizeMB,
		UploadChunkSizeMB:      s.UploadChunkSizeMB,
		UploadSessionTTLHours:  s.UploadSessionTTLHours,
		UploadCleanupIntervalS: s.UploadCleanupIntervalS,
		NtfyURL:                s.NtfyURL,
		NtfyTopic:              s.NtfyTopic,
		NtfyMessageTemplate:    s.NtfyMessageTemplate,
		NtfyMatchEdgeID:        s.NtfyMatchEdgeID,
		NtfyMatchEdgeInstID:    s.NtfyMatchEdgeInstID,
		NtfyMatchSource:        s.NtfyMatchSource,
		HookPreCommand:         s.HookPreCommand,
		HookPostCommand:        s.HookPostCommand,
	}
}

// BuildSettings constructs Settings from a SettingsPayload merged with environment.
func BuildSettings(p *SettingsPayload) (*Settings, error) {
	keyPath := IssuerKeyPathFromEnv()
	_, pub, err := signing.LoadOrCreateIssuerKeypair(keyPath)
	if err != nil {
		return nil, fmt.Errorf("issuer keypair: %w", err)
	}

	dbURL, err := buildIndexDatabaseURL()
	if err != nil {
		return nil, err
	}

	ntfyURL := ""
	hookPre := ""
	hookPost := ""
	theme := "dark"
	logLevel := "INFO"
	retentionKeepLast := 3
	maxUploadSizeMB := 2048
	uploadChunkSizeMB := 8
	uploadSessionTTLHours := 24
	uploadCleanupIntervalS := 300
	ntfyTopic := ""
	ntfyTemplate := ""
	ntfyMatchEdge := ""
	ntfyMatchInst := ""
	ntfyMatchSrc := ""

	if p != nil {
		if p.RetentionKeepLast > 0 {
			retentionKeepLast = maxInt(1, p.RetentionKeepLast)
		}
		if ll := strings.ToUpper(strings.TrimSpace(p.LogLevel)); ll != "" {
			logLevel = ll
		}
		theme = coerceTheme(p.Theme)
		if p.MaxUploadSizeMB > 0 {
			maxUploadSizeMB = maxInt(1, p.MaxUploadSizeMB)
		}
		if p.UploadChunkSizeMB > 0 {
			uploadChunkSizeMB = maxInt(1, p.UploadChunkSizeMB)
		}
		if p.UploadSessionTTLHours > 0 {
			uploadSessionTTLHours = maxInt(1, p.UploadSessionTTLHours)
		}
		if p.UploadCleanupIntervalS > 0 {
			uploadCleanupIntervalS = maxInt(10, p.UploadCleanupIntervalS)
		}
		var err error
		ntfyURL, err = coerceURL(p.NtfyURL)
		if err != nil {
			return nil, err
		}
		ntfyTopic = strings.TrimSpace(p.NtfyTopic)
		ntfyTemplate = strings.TrimSpace(p.NtfyMessageTemplate)
		ntfyMatchEdge = strings.TrimSpace(p.NtfyMatchEdgeID)
		ntfyMatchInst = strings.TrimSpace(p.NtfyMatchEdgeInstID)
		ntfyMatchSrc = strings.TrimSpace(p.NtfyMatchSource)
		hookPre = strings.TrimSpace(p.HookPreCommand)
		hookPost = strings.TrimSpace(p.HookPostCommand)
	}

	port := coerceInt(os.Getenv("HTTP_PORT"), 6555, 1)

	return &Settings{
		IssuerKeyPath:          keyPath,
		IssuerPublicKey:        pub,
		StorageBackend:         coerceText(strings.ToLower(os.Getenv("STORAGE_BACKEND")), "local"),
		IndexDatabaseURL:       dbURL,
		BackupRoot:             coerceText(os.Getenv("BACKUP_ROOT"), "/backups"),
		StagingDir:             coerceText(os.Getenv("STAGING_DIR"), "/staging"),
		RetentionKeepLast:      retentionKeepLast,
		LogLevel:               logLevel,
		Theme:                  theme,
		MaxUploadSizeMB:        maxUploadSizeMB,
		UploadChunkSizeMB:      uploadChunkSizeMB,
		UploadSessionTTLHours:  uploadSessionTTLHours,
		UploadCleanupIntervalS: uploadCleanupIntervalS,
		NtfyURL:                ntfyURL,
		NtfyTopic:              ntfyTopic,
		NtfyMessageTemplate:    ntfyTemplate,
		NtfyMatchEdgeID:        ntfyMatchEdge,
		NtfyMatchEdgeInstID:    ntfyMatchInst,
		NtfyMatchSource:        ntfyMatchSrc,
		HookPreCommand:         hookPre,
		HookPostCommand:        hookPost,
		HTTPHost:               coerceText(os.Getenv("HTTP_HOST"), "0.0.0.0"),
		HTTPPort:               port,
	}, nil
}

func buildIndexDatabaseURL() (string, error) {
	if v := strings.TrimSpace(os.Getenv("INDEX_DATABASE_URL")); v != "" {
		return v, nil
	}
	username := firstNonEmpty(os.Getenv("INDEX_DATABASE_USER"), os.Getenv("POSTGRES_USER"))
	password := firstNonEmpty(os.Getenv("INDEX_DATABASE_PASSWORD"), os.Getenv("POSTGRES_PASSWORD"))
	if username == "" || password == "" {
		return "", fmt.Errorf("central requires PostgreSQL credentials; set INDEX_DATABASE_URL or POSTGRES_USER and POSTGRES_PASSWORD")
	}
	host := coerceText(os.Getenv("INDEX_DATABASE_HOST"), "postgres")
	port := coerceText(os.Getenv("INDEX_DATABASE_PORT"), "5432")
	db := firstNonEmpty(os.Getenv("INDEX_DATABASE_NAME"), os.Getenv("POSTGRES_DB"), "three_to_one_go")
	return fmt.Sprintf("postgresql://%s:%s@%s:%s/%s",
		url.QueryEscape(username), url.QueryEscape(password), host, port, db), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
