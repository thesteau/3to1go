package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/3to1go/edge/internal/config"
)

const DefaultNtfyMessageTemplate = "Edge uploaded {{ edge_id }}/{{ edge_instance_id }} job {{ job_name }} to Central as {{ stored_as }}."

var templatePattern = regexp.MustCompile(`{{\s*([a-zA-Z0-9_]+)\s*}}`)

// NtfyPublisher sends upload notifications to a ntfy server.
type NtfyPublisher struct {
	logger *slog.Logger
}

func NewNtfyPublisher(logger *slog.Logger) *NtfyPublisher {
	return &NtfyPublisher{logger: logger}
}

func (n *NtfyPublisher) SetLogger(logger *slog.Logger) {
	n.logger = logger
}

func (n *NtfyPublisher) Snapshot(cfg *config.Settings) map[string]any {
	return map[string]any{
		"ntfy_url":                 cfg.NtfyURL,
		"ntfy_topic":               cfg.NtfyTopic,
		"ntfy_message_template":    cfg.NtfyMessageTemplate,
		"default_message_template": DefaultNtfyMessageTemplate,
	}
}

func (n *NtfyPublisher) PublishTest(ntfyURL, ntfyTopic, messageTemplate string) error {
	tmpl := strings.TrimSpace(messageTemplate)
	if tmpl == "" {
		tmpl = DefaultNtfyMessageTemplate
	}
	message := RenderNtfyMessage(tmpl, map[string]string{
		"edge_id":          "edge-01",
		"edge_instance_id": "edgeinstance0001",
		"job_name":         "test-job",
		"stored_as":        "test-upload.tar.zst",
	})
	return publish(ntfyURL, ntfyTopic, message)
}

func (n *NtfyPublisher) PublishBestEffort(cfg *config.Settings, context map[string]string) {
	if cfg.NtfyURL == "" || cfg.NtfyTopic == "" {
		return
	}
	tmpl := cfg.NtfyMessageTemplate
	if tmpl == "" {
		tmpl = DefaultNtfyMessageTemplate
	}
	message := RenderNtfyMessage(tmpl, context)
	if err := publish(cfg.NtfyURL, cfg.NtfyTopic, message); err != nil {
		n.logger.Warn("ntfy_publish_failed",
			"edge_id", context["edge_id"],
			"job_name", context["job_name"],
			"detail", err)
	}
}

// RenderNtfyMessage replaces {{ key }} placeholders with values from context.
func RenderNtfyMessage(template string, context map[string]string) string {
	normalized := strings.TrimSpace(template)
	if normalized == "" {
		normalized = DefaultNtfyMessageTemplate
	}
	return templatePattern.ReplaceAllStringFunc(normalized, func(match string) string {
		sub := templatePattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		if v, ok := context[sub[1]]; ok {
			return v
		}
		return ""
	})
}

func publish(ntfyURL, ntfyTopic, message string) error {
	base := strings.TrimRight(strings.TrimSpace(ntfyURL), "/")
	topic := strings.TrimSpace(ntfyTopic)
	if base == "" || topic == "" {
		return fmt.Errorf("ntfy url and topic are required")
	}

	publishURL := base + "/" + url.PathEscape(topic)
	payload := []byte(message)
	req, err := http.NewRequest(http.MethodPost, publishURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(payload)))
	req.Header.Set("X-Relay-Event", "upload-finished")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("unable to reach ntfy server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		detail := extractNtfyError(body)
		if detail == "" {
			detail = fmt.Sprintf("ntfy returned %d", resp.StatusCode)
		}
		return fmt.Errorf("%s", detail)
	}
	return nil
}

func extractNtfyError(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err == nil {
		for _, key := range []string{"error", "message"} {
			if s, ok := m[key].(string); ok && s != "" {
				return s
			}
		}
	}
	return strings.TrimSpace(string(body))
}
