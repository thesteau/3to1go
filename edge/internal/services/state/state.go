package state

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"sync"
)

const stateFilename = "edge-state.json"

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
}

// StateStore is a thread-safe, JSON-backed store for JobState values.
type StateStore struct {
	mu       sync.RWMutex
	stateDir string
	path     string
	data     map[string]JobState
}

// NewStateStore opens (or creates) the state store in stateDir.
func NewStateStore(stateDir string) (*StateStore, error) {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, err
	}
	s := &StateStore{
		stateDir: stateDir,
		path:     filepath.Join(stateDir, stateFilename),
	}
	s.data = s.load()
	return s, nil
}

// Get returns a copy of the JobState for key (zero-value if not present).
func (s *StateStore) Get(key string) JobState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[key]
}

// Set stores a copy of state for key and persists to disk.
func (s *StateStore) Set(key string, state JobState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = state
	return s.saveLocked()
}

// Delete removes the state for key and persists to disk.
func (s *StateStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; !ok {
		return nil
	}
	delete(s.data, key)
	return s.saveLocked()
}

// ReferencedPendingArchives returns the set of archive paths currently referenced by any job state.
func (s *StateStore) ReferencedPendingArchives() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]bool)
	for _, st := range s.data {
		if st.PendingArchive != "" {
			result[st.PendingArchive] = true
		}
	}
	return result
}

// Snapshot returns a deep copy of the full state map.
func (s *StateStore) Snapshot() map[string]JobState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]JobState, len(s.data))
	maps.Copy(out, s.data)
	return out
}

// ClearManualInterventions resets all jobs requiring manual intervention and returns the count.
func (s *StateStore) ClearManualInterventions() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for key, st := range s.data {
		if !st.ManualInterventionRequired {
			continue
		}
		st.ManualInterventionRequired = false
		st.NextRetryAt = ""
		st.LastStatus = "manual_retry_requested"
		s.data[key] = st
		count++
	}
	if count == 0 {
		return 0, nil
	}
	return count, s.saveLocked()
}

// ClearManualIntervention resets a single job and returns true if it was pending intervention.
func (s *StateStore) ClearManualIntervention(key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.data[key]
	if !ok || !st.ManualInterventionRequired {
		return false, nil
	}
	st.ManualInterventionRequired = false
	st.NextRetryAt = ""
	st.LastStatus = "manual_retry_requested"
	s.data[key] = st
	return true, s.saveLocked()
}

func (s *StateStore) saveLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *StateStore) load() map[string]JobState {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return make(map[string]JobState)
	}
	var data map[string]JobState
	if err := json.Unmarshal(raw, &data); err != nil {
		return make(map[string]JobState)
	}
	return data
}
