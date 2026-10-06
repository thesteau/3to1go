package configutil

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

func CoerceText(value, def string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return def
	}
	return v
}

func CoerceTheme(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "light" {
		return "light"
	}
	return "dark"
}

func UsesContainerLayout() bool {
	return strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")) == "/config"
}

func CoerceURL(value, def string) (string, error) {
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

func ParseLogLevel(level string) slog.Level {
	switch level {
	case "DEBUG":
		return slog.LevelDebug
	case "WARNING", "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
