package ntfy

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/3to1go/central/internal/config"
	notification "github.com/3to1go/shared/ntfy"
)

const DefaultNtfyMessageTemplate = "Central received {{ edge_id }}/{{ edge_instance_id }} job {{ job_name }} from {{ advertised_url }} as {{ stored_as }}."

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

func (n *NtfyPublisher) Snapshot(s *config.Settings) map[string]any {
	return map[string]any{
		"ntfy_url":                    s.NtfyURL,
		"ntfy_topic":                  s.NtfyTopic,
		"ntfy_message_template":       s.NtfyMessageTemplate,
		"ntfy_match_edge_id":          s.NtfyMatchEdgeID,
		"ntfy_match_edge_instance_id": s.NtfyMatchEdgeInstID,
		"ntfy_match_source":           s.NtfyMatchSource,
		"default_message_template":    DefaultNtfyMessageTemplate,
	}
}

func (n *NtfyPublisher) PublishTest(cfg map[string]any) error {
	tmpl := strings.TrimSpace(fmt.Sprintf("%v", orEmpty(cfg["ntfy_message_template"])))
	if tmpl == "" {
		tmpl = DefaultNtfyMessageTemplate
	}
	msg := RenderMessage(tmpl, map[string]any{
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

func (n *NtfyPublisher) PublishBestEffort(s *config.Settings, ctx map[string]any) {
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

// PublishUnusualUpload alerts that an archive's size differs sharply from the
// job's history. It honors the same Edge and source filters as upload notices,
// but uses its own message and a high priority.
func (n *NtfyPublisher) PublishUnusualUpload(s *config.Settings, ctx map[string]any) {
	if !n.matches(s, ctx) {
		return
	}
	msg := fmt.Sprintf("Central received an unusual backup from %s/%s job %s. %s",
		ctxString(ctx, "edge_id"), ctxString(ctx, "edge_instance_id"), ctxString(ctx, "job_name"), ctxString(ctx, "unusual"))
	err := n.publishWithHeaders(s.NtfyURL, s.NtfyTopic, msg, map[string]string{
		"X-Relay-Event": "unusual-upload",
		"Priority":      "high",
		"Tags":          "warning",
	})
	if err != nil {
		n.logger.Warn("ntfy_publish_failed", "edge_id", ctx["edge_id"], "job_name", ctx["job_name"], "error", err)
	}
}

func RenderMessage(template string, ctx map[string]any) string {
	return notification.Render(template, DefaultNtfyMessageTemplate, func(key string) string { return ctxString(ctx, key) })
}

func (n *NtfyPublisher) matches(s *config.Settings, ctx map[string]any) bool {
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

func ctxString(ctx map[string]any, key string) string {
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
	return n.publishWithHeaders(ntfyURL, ntfyTopic, message, map[string]string{"X-Relay-Event": "upload-received"})
}

func (n *NtfyPublisher) publishWithHeaders(ntfyURL, ntfyTopic, message string, headers map[string]string) error {
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
	for name, value := range headers {
		req.Header.Set(name, value)
	}

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

func orEmpty(v any) any {
	if v == nil {
		return ""
	}
	return v
}

func orDefault(v any, def string) any {
	if v == nil {
		return def
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if s == "" {
		return def
	}
	return s
}
