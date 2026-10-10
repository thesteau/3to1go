package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func render(template string, event Event) string {
	if template == "" {
		template = "{{ app }} {{ event }}: {{ scout_id }}/{{ scout_instance_id }} job {{ job_name }} ({{ status }})."
	}
	fields := map[string]string{"app": event.App, "event": event.Type, "scout_id": event.ScoutID, "scout_instance_id": event.ScoutInstanceID, "job_name": event.JobName, "status": event.Status, "stored_as": event.StoredAs, "error_category": event.ErrorCategory, "detail": event.Detail, "time": event.Time}
	// Replace tokens in one pass so inserted values cannot become template instructions.
	return renderRest(template, fields)
}

func renderRest(template string, fields map[string]string) string {
	var result strings.Builder
	for {
		start := strings.Index(template, "{{")
		if start < 0 {
			result.WriteString(template)
			return result.String()
		}
		end := strings.Index(template[start+2:], "}}")
		if end < 0 {
			result.WriteString(template)
			return result.String()
		}
		end += start + 2
		result.WriteString(template[:start])
		result.WriteString(fields[strings.TrimSpace(template[start+2:end])])
		template = template[end+2:]
	}
}

func (m *Manager) deliver(parent context.Context, d storedDestination, event Event) error {
	endpoint, parseErr := url.Parse(d.URL)
	if parseErr != nil || endpoint.Scheme != "https" {
		return errors.New("destination requires HTTPS; replace the stored URL")
	}
	if !d.IncludeDetail {
		event.Detail = ""
	}
	message := render(d.MessageTemplate, event)
	contentType := "application/json"
	var payload []byte
	switch d.Format {
	case "text":
		payload, contentType = []byte(message), "text/plain; charset=utf-8"
	case "discord":
		// Disable mentions even when a job name contains @everyone or a role mention.
		characters := []rune(message)
		message = string(characters[:min(len(characters), 2000)])
		payload, _ = json.Marshal(map[string]any{"content": message, "allowed_mentions": map[string]any{"parse": []string{}}})
	default:
		payload, _ = json.Marshal(struct {
			Event
			Message string `json:"message"`
		}{event, message})
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(d.TimeoutSeconds)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(payload))
	if err != nil {
		return errors.New("cannot create notification request")
	}
	for name, value := range d.Headers {
		req.Header.Set(name, value)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-3to1go-Event", event.Type)
	req.Header.Set("X-3to1go-Event-ID", event.ID)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if m.tlsConfig != nil {
		transport.TLSClientConfig = m.tlsConfig()
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("notification timed out")
		}
		return errors.New("notification connection failed; check the destination and trusted certificates")
	}
	defer func() { _ = response.Body.Close() }()
	// Do not read or return response bodies: receivers can echo request credentials.
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("notification returned HTTP %d", response.StatusCode)
	}
	return nil
}
