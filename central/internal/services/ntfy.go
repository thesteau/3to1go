package services

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/3to1go/central/internal/config"
)

const DefaultNtfyMessageTemplate = "Central received {{ edge_id }}/{{ edge_instance_id }} job {{ job_name }} from {{ advertised_url }} as {{ stored_as }}."

var templatePattern = regexp.MustCompile(`{{\s*([a-zA-Z0-9_]+)\s*}}`)

type NtfyPublisher struct {
	logger *slog.Logger
	client *http.Client
}

func NewNtfyPublisher(logger *slog.Logger) *NtfyPublisher {
	return &NtfyPublisher{
		logger: logger,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *NtfyPublisher) Snapshot(s *config.Settings) map[string]interface{} {
	return map[string]interface{}{
		"ntfy_url":                    s.NtfyURL,
		"ntfy_topic":                  s.NtfyTopic,
		"ntfy_message_template":       s.NtfyMessageTemplate,
		"ntfy_match_edge_id":          s.NtfyMatchEdgeID,
		"ntfy_match_edge_instance_id": s.NtfyMatchEdgeInstID,
		"ntfy_match_source":           s.NtfyMatchSource,
		"default_message_template":    DefaultNtfyMessageTemplate,
	}
}

func (n *NtfyPublisher) PublishTest(cfg map[string]interface{}) error {
	tmpl := strings.TrimSpace(fmt.Sprintf("%v", orEmpty(cfg["ntfy_message_template"])))
	if tmpl == "" {
		tmpl = DefaultNtfyMessageTemplate
	}
	msg := RenderMessage(tmpl, map[string]interface{}{
		"edge_id":          orDefault(cfg["ntfy_match_edge_id"], "edge-01"),
		"edge_instance_id": orDefault(cfg["ntfy_match_edge_instance_id"], "edgeinstance0001"),
		"job_name":         "test-job",
		"advertised_url":   "https://edge.example.com",
		"source_address":   orDefault(cfg["ntfy_match_source"], "127.0.0.1"),
		"stored_as":        "test-upload.tar.zst",
	})
	return n.publish(
		strings.TrimSpace(fmt.Sprintf("%v", orEmpty(cfg["ntfy_url"]))),
		strings.TrimSpace(fmt.Sprintf("%v", orEmpty(cfg["ntfy_topic"]))),
		msg,
	)
}

func (n *NtfyPublisher) PublishBestEffort(s *config.Settings, ctx map[string]interface{}) {
	if !n.matches(s, ctx) {
		return
	}
	tmpl := s.NtfyMessageTemplate
	if tmpl == "" {
		tmpl = DefaultNtfyMessageTemplate
	}
	msg := RenderMessage(tmpl, ctx)
	if err := n.publish(s.NtfyURL, s.NtfyTopic, msg); err != nil {
		n.logger.Warn("ntfy_publish_failed",
			"edge_id", ctx["edge_id"],
			"edge_instance_id", ctx["edge_instance_id"],
			"job_name", ctx["job_name"],
			"error", err)
	}
}

func RenderMessage(template string, ctx map[string]interface{}) string {
	norm := strings.TrimSpace(template)
	if norm == "" {
		norm = DefaultNtfyMessageTemplate
	}
	return templatePattern.ReplaceAllStringFunc(norm, func(match string) string {
		key := strings.TrimSpace(match[2 : len(match)-2])
		return ctxString(ctx, key)
	})
}

func (n *NtfyPublisher) matches(s *config.Settings, ctx map[string]interface{}) bool {
	if s.NtfyURL == "" || s.NtfyTopic == "" {
		return false
	}
	if s.NtfyMatchEdgeID != "" && s.NtfyMatchEdgeID != ctxString(ctx, "edge_id") {
		return false
	}
	if s.NtfyMatchEdgeInstID != "" && s.NtfyMatchEdgeInstID != ctxString(ctx, "edge_instance_id") {
		return false
	}
	if s.NtfyMatchSource != "" && s.NtfyMatchSource != ctxString(ctx, "source_address") {
		return false
	}
	return true
}

func ctxString(ctx map[string]interface{}, key string) string {
	v, ok := ctx[key]
	if !ok || v == nil {
		return ""
	}
	if sp, ok := v.(*string); ok {
		if sp == nil {
			return ""
		}
		return *sp
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func (n *NtfyPublisher) publish(ntfyURL, ntfyTopic, message string) error {
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
	req.Header.Set("X-Relay-Event", "upload-received")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("unable to reach ntfy server: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ntfy returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func orEmpty(v interface{}) interface{} {
	if v == nil {
		return ""
	}
	return v
}

func orDefault(v interface{}, def string) interface{} {
	if v == nil {
		return def
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if s == "" {
		return def
	}
	return s
}
