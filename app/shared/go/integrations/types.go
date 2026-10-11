// Package integrations delivers HTTP notifications without exposing destination secrets.
package integrations

import (
	"context"
	"time"
)

const (
	JobStarted     = "job-started"
	JobFinished    = "job-finished"
	UploadStarted  = "upload-started"
	UploadFinished = "upload-finished"
	UploadReceived = "upload-received"
	UnusualBackup  = "unusual-backup"
	UnusualUpload  = "unusual-upload"
)

const DefaultMessageTemplate = "{{ app }} {{ event }}: {{ scout_id }}/{{ scout_instance_id }} job {{ job_name }} ({{ status }})."
const DefaultPayloadTemplate = "{\n  \"message\": \"{{ message }}\"\n}"

// Event contains only explicitly selected notification fields, never settings or paths.
type Event struct {
	ID              string `json:"id"`
	App             string `json:"app"`
	Type            string `json:"event"`
	Time            string `json:"time"`
	ScoutID         string `json:"scout_id,omitempty"`
	ScoutInstanceID string `json:"scout_instance_id,omitempty"`
	JobName         string `json:"job_name,omitempty"`
	Status          string `json:"status,omitempty"`
	StoredAs        string `json:"stored_as,omitempty"`
	ErrorCategory   string `json:"error_category,omitempty"`
	Detail          string `json:"detail,omitempty"`
	SourceAddress   string `json:"-"`
}

// Destination is safe to return to browsers. URL and headers are deliberately absent.
type Destination struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Enabled            bool     `json:"enabled"`
	Format             string   `json:"format"`
	Events             []string `json:"events"`
	MatchScoutID       string   `json:"match_scout_id"`
	MatchInstanceID    string   `json:"match_instance_id"`
	MatchJobName       string   `json:"match_job_name"`
	MatchSourceAddress string   `json:"match_source_address"`
	MessageTemplate    string   `json:"message_template"`
	PayloadTemplate    string   `json:"payload_template"`
	IncludeDetail      bool     `json:"include_detail"`
	TimeoutSeconds     int      `json:"timeout_seconds"`
	URLConfigured      bool     `json:"url_configured"`
	HeadersConfigured  bool     `json:"headers_configured"`
}

// Update uses omitted secret fields to preserve existing values. Empty headers clear them.
type Update struct {
	Destination
	URL     *string            `json:"url,omitempty"`
	Headers *map[string]string `json:"headers,omitempty"`
}

type storedDestination struct {
	Destination
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type Store interface {
	Snapshot() []Destination
	Save(Update) (Destination, error)
	Delete(string) error
	Test(context.Context, string) error
}

func eventsFor(app string) []string {
	if app == "scout" {
		return []string{JobStarted, JobFinished, UploadFinished, UnusualBackup}
	}
	return []string{UploadStarted, UploadReceived, UnusualUpload}
}

func isPreEvent(event string) bool { return event == JobStarted || event == UploadStarted }

func timestamp() string { return time.Now().UTC().Format(time.RFC3339) }
