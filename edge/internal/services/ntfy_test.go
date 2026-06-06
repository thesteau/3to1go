package services

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// RenderNtfyMessage
// ---------------------------------------------------------------------------

func TestRenderNtfyMessage_Substitution(t *testing.T) {
	tmpl := "Job {{ job_name }} succeeded on {{ edge_id }}."
	ctx := map[string]string{
		"job_name": "photos",
		"edge_id":  "edge-01",
	}
	got := RenderNtfyMessage(tmpl, ctx)
	want := "Job photos succeeded on edge-01."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderNtfyMessage_UnknownKeyBecomesEmpty(t *testing.T) {
	tmpl := "Hello {{ unknown_key }}."
	got := RenderNtfyMessage(tmpl, map[string]string{})
	if got != "Hello ." {
		t.Errorf("got %q, want %q", got, "Hello .")
	}
}

func TestRenderNtfyMessage_EmptyTemplateUsesDefault(t *testing.T) {
	got := RenderNtfyMessage("", map[string]string{
		"edge_id":          "edge-01",
		"edge_instance_id": "inst01",
		"job_name":         "photos",
		"stored_as":        "photos.tar.zst",
	})
	if !strings.Contains(got, "edge-01") {
		t.Errorf("expected edge_id in default template output, got %q", got)
	}
}

func TestRenderNtfyMessage_WhitespaceOnlyTemplateUsesDefault(t *testing.T) {
	got := RenderNtfyMessage("   ", map[string]string{})
	if got == "   " {
		t.Error("expected whitespace-only template to fall back to default")
	}
}

func TestRenderNtfyMessage_MultipleOccurrences(t *testing.T) {
	tmpl := "{{ name }} and {{ name }} again."
	got := RenderNtfyMessage(tmpl, map[string]string{"name": "Alice"})
	if got != "Alice and Alice again." {
		t.Errorf("got %q", got)
	}
}

func TestRenderNtfyMessage_SpacesInsideBraces(t *testing.T) {
	tmpl := "Hello {{  edge_id  }}!"
	got := RenderNtfyMessage(tmpl, map[string]string{"edge_id": "edge-42"})
	if got != "Hello edge-42!" {
		t.Errorf("got %q", got)
	}
}

func TestRenderNtfyMessage_DefaultContainsExpectedFields(t *testing.T) {
	if !strings.Contains(DefaultNtfyMessageTemplate, "{{ job_name }}") {
		t.Error("default template should reference job_name")
	}
	if !strings.Contains(DefaultNtfyMessageTemplate, "{{ edge_id }}") {
		t.Error("default template should reference edge_id")
	}
}
