package services

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/relay/central/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- RenderMessage ---

func TestRenderMessage_BasicInterpolation(t *testing.T) {
	tmpl := "Job {{ job_name }} from {{ edge_id }}."
	ctx := map[string]interface{}{
		"job_name": "backup",
		"edge_id":  "edge01",
	}
	got := RenderMessage(tmpl, ctx)
	if got != "Job backup from edge01." {
		t.Errorf("got %q", got)
	}
}

func TestRenderMessage_MissingKey(t *testing.T) {
	tmpl := "Hello {{ missing_key }} world"
	got := RenderMessage(tmpl, map[string]interface{}{})
	if got != "Hello  world" {
		t.Errorf("got %q", got)
	}
}

func TestRenderMessage_NilValue(t *testing.T) {
	tmpl := "Value: {{ key }}"
	ctx := map[string]interface{}{"key": nil}
	got := RenderMessage(tmpl, ctx)
	if got != "Value: " {
		t.Errorf("got %q", got)
	}
}

func TestRenderMessage_EmptyTemplateUsesDefault(t *testing.T) {
	got := RenderMessage("", map[string]interface{}{})
	if !strings.Contains(got, "Central received") {
		t.Errorf("expected default template, got %q", got)
	}
}

func TestRenderMessage_WhitespaceTemplateUsesDefault(t *testing.T) {
	got := RenderMessage("   ", map[string]interface{}{})
	if !strings.Contains(got, "Central received") {
		t.Errorf("expected default template, got %q", got)
	}
}

func TestRenderMessage_SpacesInPlaceholder(t *testing.T) {
	tmpl := "Hello {{  name  }}"
	ctx := map[string]interface{}{"name": "world"}
	got := RenderMessage(tmpl, ctx)
	if got != "Hello world" {
		t.Errorf("got %q", got)
	}
}

func TestRenderMessage_NoPlaceholders(t *testing.T) {
	got := RenderMessage("plain text", map[string]interface{}{})
	if got != "plain text" {
		t.Errorf("got %q", got)
	}
}

// --- matches (via PublishBestEffort) ---

func TestPublishBestEffort_NoURLSkips(t *testing.T) {
	n := NewNtfyPublisher(discardLogger())
	s := &config.Settings{NtfyURL: "", NtfyTopic: "topic"}
	// Should not panic or call anything
	n.PublishBestEffort(s, map[string]interface{}{"edge_id": "e1"})
}

func TestPublishBestEffort_NoTopicSkips(t *testing.T) {
	n := NewNtfyPublisher(discardLogger())
	s := &config.Settings{NtfyURL: "https://ntfy.sh", NtfyTopic: ""}
	n.PublishBestEffort(s, map[string]interface{}{"edge_id": "e1"})
}

func TestPublishBestEffort_EdgeIDFilterMatch(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewNtfyPublisher(discardLogger())
	s := &config.Settings{
		NtfyURL:         srv.URL,
		NtfyTopic:       "alerts",
		NtfyMatchEdgeID: "edge01",
	}
	n.PublishBestEffort(s, map[string]interface{}{"edge_id": "edge01"})
	if !called {
		t.Error("expected ntfy to be called when edge_id matches")
	}
}

func TestPublishBestEffort_EdgeIDFilterNoMatch(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewNtfyPublisher(discardLogger())
	s := &config.Settings{
		NtfyURL:         srv.URL,
		NtfyTopic:       "alerts",
		NtfyMatchEdgeID: "edge01",
	}
	n.PublishBestEffort(s, map[string]interface{}{"edge_id": "other-edge"})
	if called {
		t.Error("expected ntfy to be skipped when edge_id doesn't match")
	}
}

func TestPublishBestEffort_InstIDFilterNoMatch(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewNtfyPublisher(discardLogger())
	s := &config.Settings{
		NtfyURL:             srv.URL,
		NtfyTopic:           "alerts",
		NtfyMatchEdgeInstID: "inst1",
	}
	n.PublishBestEffort(s, map[string]interface{}{"edge_instance_id": "inst2"})
	if called {
		t.Error("expected ntfy to be skipped when instance_id doesn't match")
	}
}

func TestPublishBestEffort_SourceFilterNoMatch(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewNtfyPublisher(discardLogger())
	s := &config.Settings{
		NtfyURL:         srv.URL,
		NtfyTopic:       "alerts",
		NtfyMatchSource: "1.2.3.4",
	}
	n.PublishBestEffort(s, map[string]interface{}{"source_address": "5.6.7.8"})
	if called {
		t.Error("expected ntfy to be skipped when source doesn't match")
	}
}

func TestPublishBestEffort_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	n := NewNtfyPublisher(discardLogger())
	s := &config.Settings{NtfyURL: srv.URL, NtfyTopic: "alerts"}
	// Should not panic, just log the error
	n.PublishBestEffort(s, map[string]interface{}{})
}

// --- publish ---

func TestPublish_UnreachableServer(t *testing.T) {
	n := NewNtfyPublisher(discardLogger())
	// Port 1 is essentially always unreachable
	err := n.publish("http://127.0.0.1:1", "topic", "msg")
	if err == nil || !strings.Contains(err.Error(), "unable to reach") {
		t.Errorf("expected unreachable error, got %v", err)
	}
}

func TestPublish_MissingURLOrTopic(t *testing.T) {
	n := NewNtfyPublisher(discardLogger())
	if err := n.publish("", "topic", "msg"); err == nil {
		t.Error("expected error for empty URL")
	}
	if err := n.publish("https://ntfy.sh", "", "msg"); err == nil {
		t.Error("expected error for empty topic")
	}
}

func TestPublish_Success(t *testing.T) {
	var gotMsg string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotMsg = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewNtfyPublisher(discardLogger())
	if err := n.publish(srv.URL, "mytopic", "hello ntfy"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if gotMsg != "hello ntfy" {
		t.Errorf("got message %q, want 'hello ntfy'", gotMsg)
	}
}

func TestPublish_400Response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	n := NewNtfyPublisher(discardLogger())
	err := n.publish(srv.URL, "topic", "msg")
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Errorf("expected 400 error, got %v", err)
	}
}

func TestPublish_TopicEncoded(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.RequestURI // raw, unmodified request URI from the wire
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewNtfyPublisher(discardLogger())
	n.publish(srv.URL, "my topic", "msg")
	if !strings.Contains(gotURI, "my%20topic") {
		t.Errorf("topic not URL-encoded, got request URI %q", gotURI)
	}
}

// --- PublishTest ---

func TestPublishTest_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewNtfyPublisher(discardLogger())
	cfg := map[string]interface{}{
		"ntfy_url":              srv.URL,
		"ntfy_topic":            "testtopic",
		"ntfy_message_template": "Test {{ job_name }}",
	}
	if err := n.PublishTest(cfg); err != nil {
		t.Fatalf("PublishTest: %v", err)
	}
}

func TestPublishTest_DefaultTemplate(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewNtfyPublisher(discardLogger())
	cfg := map[string]interface{}{
		"ntfy_url":   srv.URL,
		"ntfy_topic": "testtopic",
	}
	n.PublishTest(cfg)
	if !strings.Contains(gotBody, "Central received") {
		t.Errorf("expected default template in body, got %q", gotBody)
	}
}

// --- Snapshot ---

func TestNtfySnapshot(t *testing.T) {
	n := NewNtfyPublisher(discardLogger())
	s := &config.Settings{
		NtfyURL:   "https://ntfy.sh",
		NtfyTopic: "topic",
	}
	snap := n.Snapshot(s)
	if snap["ntfy_url"] != "https://ntfy.sh" {
		t.Errorf("snapshot ntfy_url missing: %v", snap)
	}
	if snap["default_message_template"] != DefaultNtfyMessageTemplate {
		t.Errorf("snapshot missing default_message_template")
	}
}

// --- orEmpty / orDefault helpers ---

func TestOrEmpty_Nil(t *testing.T) {
	if got := orEmpty(nil); got != "" {
		t.Errorf("got %v, want ''", got)
	}
}

func TestOrEmpty_Value(t *testing.T) {
	if got := orEmpty("hello"); got != "hello" {
		t.Errorf("got %v, want hello", got)
	}
}

func TestOrDefault_Nil(t *testing.T) {
	if got := orDefault(nil, "fallback"); got != "fallback" {
		t.Errorf("got %v, want fallback", got)
	}
}

func TestOrDefault_Empty(t *testing.T) {
	if got := orDefault("  ", "fallback"); got != "fallback" {
		t.Errorf("got %v, want fallback", got)
	}
}

func TestOrDefault_Value(t *testing.T) {
	if got := orDefault("real", "fallback"); got != "real" {
		t.Errorf("got %v, want real", got)
	}
}
