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
		template = DefaultMessageTemplate
	}
	// Replace tokens in one pass so inserted values cannot become template instructions.
	return renderRest(template, templateFields(event))
}

func templateFields(event Event) map[string]string {
	return map[string]string{"app": event.App, "event": event.Type, "scout_id": event.ScoutID, "scout_instance_id": event.ScoutInstanceID, "job_name": event.JobName, "status": event.Status, "stored_as": event.StoredAs, "error_category": event.ErrorCategory, "detail": event.Detail, "time": event.Time}
}

// Parse JSON before substituting string values so event data cannot change its structure.
func renderJSON(template string, fields map[string]string) ([]byte, error) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(template))
	decoder.UseNumber()
	if !json.Valid([]byte(template)) || decoder.Decode(&value) != nil {
		return nil, errors.New("payload template must be valid JSON with placeholders inside string values")
	}
	return json.Marshal(renderJSONValue(value, fields))
}

func renderJSONValue(value any, fields map[string]string) any {
	switch value := value.(type) {
	case string:
		return renderRest(value, fields)
	case map[string]any:
		for key, item := range value {
			value[key] = renderJSONValue(item, fields)
		}
		return value
	case []any:
		for i, item := range value {
			value[i] = renderJSONValue(item, fields)
		}
		return value
	default:
		return value
	}
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
	case "custom-json":
		fields := templateFields(event)
		fields["message"] = message
		template := d.PayloadTemplate
		if template == "" {
			template = DefaultPayloadTemplate
		}
		var err error
		payload, err = renderJSON(template, fields)
		if err != nil {
			return err
		}
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
