package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
)

// JobState holds the persistent state for a single backup job.
type JobState struct {
	JobName                    string `json:"job_name,omitempty"`
	LastSuccessfulFingerprint  string `json:"last_successful_fingerprint,omitempty"`
	LastSuccessfulUpload       string `json:"last_successful_upload,omitempty"`
	PendingArchive             string `json:"pending_archive,omitempty"`
	PendingArchiveSize         *int64 `json:"pending_archive_size,omitempty"`
	PendingArchiveSHA256       string `json:"pending_archive_sha256,omitempty"`
	PendingFingerprint         string `json:"pending_fingerprint,omitempty"`
	PendingTimestamp           string `json:"pending_timestamp,omitempty"`
	UploadID                   string `json:"upload_id,omitempty"`
	UploadOffset               int64  `json:"upload_offset"`
	UploadAttemptCount         int    `json:"upload_attempt_count"`
	CurrentChunkSizeBytes      *int64 `json:"current_chunk_size_bytes,omitempty"`
	NextRetryAt                string `json:"next_retry_at,omitempty"`
	LastErrorDetail            string `json:"last_error_detail,omitempty"`
	LastErrorCategory          string `json:"last_error_category,omitempty"`
	LastUploadStartedAt        string `json:"last_upload_started_at,omitempty"`
	LastUploadUpdatedAt        string `json:"last_upload_updated_at,omitempty"`
	ActivePhase                string `json:"active_phase,omitempty"`
	ActivePhasePercent         int    `json:"active_phase_percent"`
	ManualInterventionRequired bool   `json:"manual_intervention_required"`
	LastStatus                 string `json:"last_status,omitempty"`
	LastStoredAs               string `json:"last_stored_as,omitempty"`
	LastPruned                 int    `json:"last_pruned"`
	LastDuplicate              bool   `json:"last_duplicate"`
	LastBackupSizeBytes        *int64 `json:"last_backup_size_bytes,omitempty"`
}

// StateStore is a SQLite-backed store for JobState values.
type StateStore struct {
	db *sql.DB
}

// NewStateStore creates a StateStore backed by the given database.
func NewStateStore(db *sql.DB) *StateStore {
	return &StateStore{db: db}
}

// EnsureSchema creates the job_states table if it does not exist.
func (s *StateStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS job_states (
			key TEXT PRIMARY KEY,
			job_name TEXT NOT NULL DEFAULT '',
			last_successful_fingerprint TEXT NOT NULL DEFAULT '',
			last_successful_upload TEXT NOT NULL DEFAULT '',
			pending_archive TEXT NOT NULL DEFAULT '',
			pending_archive_size INTEGER,
			pending_archive_sha256 TEXT NOT NULL DEFAULT '',
			pending_fingerprint TEXT NOT NULL DEFAULT '',
			pending_timestamp TEXT NOT NULL DEFAULT '',
			upload_id TEXT NOT NULL DEFAULT '',
			upload_offset INTEGER NOT NULL DEFAULT 0,
			upload_attempt_count INTEGER NOT NULL DEFAULT 0,
			current_chunk_size_bytes INTEGER,
			next_retry_at TEXT NOT NULL DEFAULT '',
			last_error_detail TEXT NOT NULL DEFAULT '',
			last_error_category TEXT NOT NULL DEFAULT '',
			last_upload_started_at TEXT NOT NULL DEFAULT '',
			last_upload_updated_at TEXT NOT NULL DEFAULT '',
			active_phase TEXT NOT NULL DEFAULT '',
			active_phase_percent INTEGER NOT NULL DEFAULT 0,
			manual_intervention_required INTEGER NOT NULL DEFAULT 0,
			last_status TEXT NOT NULL DEFAULT '',
			last_stored_as TEXT NOT NULL DEFAULT '',
			last_pruned INTEGER NOT NULL DEFAULT 0,
			last_duplicate INTEGER NOT NULL DEFAULT 0,
			last_backup_size_bytes INTEGER
		)`)
	return err
}

// MigrateFromFile imports job states from a legacy JSON file if it exists.
// On success it renames the file to prevent re-import on subsequent startups.
func (s *StateStore) MigrateFromFile(path string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var data map[string]JobState
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	for key, js := range data {
		if err := s.Set(key, js); err != nil {
			return err
		}
	}
	return os.Rename(path, path+".migrated")
}

const selectCols = `key,
	job_name, last_successful_fingerprint, last_successful_upload,
	pending_archive, pending_archive_size, pending_archive_sha256,
	pending_fingerprint, pending_timestamp, upload_id, upload_offset,
	upload_attempt_count, current_chunk_size_bytes, next_retry_at,
	last_error_detail, last_error_category, last_upload_started_at,
	last_upload_updated_at, active_phase, active_phase_percent,
	manual_intervention_required, last_status, last_stored_as,
	last_pruned, last_duplicate, last_backup_size_bytes`

func scanJobState(scan func(...any) error) (string, JobState, error) {
	var key string
	var js JobState
	var pendingArchiveSize, currentChunkSizeBytes, lastBackupSizeBytes sql.NullInt64
	var manualIntervention, lastDuplicate int
	err := scan(
		&key,
		&js.JobName, &js.LastSuccessfulFingerprint, &js.LastSuccessfulUpload,
		&js.PendingArchive, &pendingArchiveSize, &js.PendingArchiveSHA256,
		&js.PendingFingerprint, &js.PendingTimestamp, &js.UploadID, &js.UploadOffset,
		&js.UploadAttemptCount, &currentChunkSizeBytes, &js.NextRetryAt,
		&js.LastErrorDetail, &js.LastErrorCategory, &js.LastUploadStartedAt,
		&js.LastUploadUpdatedAt, &js.ActivePhase, &js.ActivePhasePercent,
		&manualIntervention, &js.LastStatus, &js.LastStoredAs,
		&js.LastPruned, &lastDuplicate, &lastBackupSizeBytes,
	)
	if err != nil {
		return "", JobState{}, err
	}
	if pendingArchiveSize.Valid {
		js.PendingArchiveSize = &pendingArchiveSize.Int64
	}
	if currentChunkSizeBytes.Valid {
		js.CurrentChunkSizeBytes = &currentChunkSizeBytes.Int64
	}
	if lastBackupSizeBytes.Valid {
		js.LastBackupSizeBytes = &lastBackupSizeBytes.Int64
	}
	js.ManualInterventionRequired = manualIntervention != 0
	js.LastDuplicate = lastDuplicate != 0
	return key, js, nil
}

// Get returns the JobState for key, or a zero value if not present.
func (s *StateStore) Get(key string) JobState {
	row := s.db.QueryRow(`SELECT `+selectCols+` FROM job_states WHERE key = ?`, key)
	_, js, err := scanJobState(row.Scan)
	if err != nil {
		return JobState{}
	}
	return js
}

// Set upserts the JobState for key.
func (s *StateStore) Set(key string, js JobState) error {
	_, err := s.db.Exec(`
		INSERT INTO job_states (key,
			job_name, last_successful_fingerprint, last_successful_upload,
			pending_archive, pending_archive_size, pending_archive_sha256,
			pending_fingerprint, pending_timestamp, upload_id, upload_offset,
			upload_attempt_count, current_chunk_size_bytes, next_retry_at,
			last_error_detail, last_error_category, last_upload_started_at,
			last_upload_updated_at, active_phase, active_phase_percent,
			manual_intervention_required, last_status, last_stored_as,
			last_pruned, last_duplicate, last_backup_size_bytes
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(key) DO UPDATE SET
			job_name = excluded.job_name,
			last_successful_fingerprint = excluded.last_successful_fingerprint,
			last_successful_upload = excluded.last_successful_upload,
			pending_archive = excluded.pending_archive,
			pending_archive_size = excluded.pending_archive_size,
			pending_archive_sha256 = excluded.pending_archive_sha256,
			pending_fingerprint = excluded.pending_fingerprint,
			pending_timestamp = excluded.pending_timestamp,
			upload_id = excluded.upload_id,
			upload_offset = excluded.upload_offset,
			upload_attempt_count = excluded.upload_attempt_count,
			current_chunk_size_bytes = excluded.current_chunk_size_bytes,
			next_retry_at = excluded.next_retry_at,
			last_error_detail = excluded.last_error_detail,
			last_error_category = excluded.last_error_category,
			last_upload_started_at = excluded.last_upload_started_at,
			last_upload_updated_at = excluded.last_upload_updated_at,
			active_phase = excluded.active_phase,
			active_phase_percent = excluded.active_phase_percent,
			manual_intervention_required = excluded.manual_intervention_required,
			last_status = excluded.last_status,
			last_stored_as = excluded.last_stored_as,
			last_pruned = excluded.last_pruned,
			last_duplicate = excluded.last_duplicate,
			last_backup_size_bytes = excluded.last_backup_size_bytes`,
		key,
		js.JobName, js.LastSuccessfulFingerprint, js.LastSuccessfulUpload,
		js.PendingArchive, js.PendingArchiveSize, js.PendingArchiveSHA256,
		js.PendingFingerprint, js.PendingTimestamp, js.UploadID, js.UploadOffset,
		js.UploadAttemptCount, js.CurrentChunkSizeBytes, js.NextRetryAt,
		js.LastErrorDetail, js.LastErrorCategory, js.LastUploadStartedAt,
		js.LastUploadUpdatedAt, js.ActivePhase, js.ActivePhasePercent,
		boolToInt(js.ManualInterventionRequired), js.LastStatus, js.LastStoredAs,
		js.LastPruned, boolToInt(js.LastDuplicate), js.LastBackupSizeBytes,
	)
	return err
}

// Delete removes the state for key (no-op if not present).
func (s *StateStore) Delete(key string) error {
	_, err := s.db.Exec(`DELETE FROM job_states WHERE key = ?`, key)
	return err
}

// ReferencedPendingArchives returns the set of archive paths currently referenced by any job.
func (s *StateStore) ReferencedPendingArchives() map[string]bool {
	rows, err := s.db.Query(`SELECT pending_archive FROM job_states WHERE pending_archive != ''`)
	if err != nil {
		return map[string]bool{}
	}
	defer rows.Close()
	result := make(map[string]bool)
	for rows.Next() {
		var archive string
		if rows.Scan(&archive) == nil && archive != "" {
			result[archive] = true
		}
	}
	return result
}

// Snapshot returns a copy of all job states keyed by path.
func (s *StateStore) Snapshot() map[string]JobState {
	rows, err := s.db.Query(`SELECT ` + selectCols + ` FROM job_states`)
	if err != nil {
		return map[string]JobState{}
	}
	defer rows.Close()
	out := make(map[string]JobState)
	for rows.Next() {
		key, js, err := scanJobState(rows.Scan)
		if err == nil {
			out[key] = js
		}
	}
	return out
}

// ClearManualInterventions resets all jobs requiring manual intervention and returns the count.
func (s *StateStore) ClearManualInterventions() (int, error) {
	res, err := s.db.Exec(`
		UPDATE job_states
		SET manual_intervention_required = 0, next_retry_at = '', last_status = 'manual_retry_requested'
		WHERE manual_intervention_required = 1`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ClearManualIntervention resets a single job; returns true if it was pending intervention.
func (s *StateStore) ClearManualIntervention(key string) (bool, error) {
	res, err := s.db.Exec(`
		UPDATE job_states
		SET manual_intervention_required = 0, next_retry_at = '', last_status = 'manual_retry_requested'
		WHERE key = ? AND manual_intervention_required = 1`, key)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
