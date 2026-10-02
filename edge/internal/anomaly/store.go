package anomaly

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/3to1go/shared/anomaly"
)

// Modes for Edge's Unusual backups setting.
const (
	ModeHold  = anomaly.ModeHold
	ModeAlert = anomaly.ModeAlert
	ModeOff   = anomaly.ModeOff
)

// Store keeps each job's accepted backup history and the observation for the
// archive currently staged. A staged observation joins the history only once
// its archive uploads, so a held anomaly never becomes the new "normal" unless
// the operator approves it.
// A nil *Store is valid and records nothing.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) EnsureSchema(ctx context.Context) error {
	if s == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS job_anomaly (
			key TEXT PRIMARY KEY,
			history TEXT NOT NULL DEFAULT '[]',
			pending TEXT NOT NULL DEFAULT ''
		)`)
	return err
}

// History returns the job's accepted observations, oldest first.
func (s *Store) History(key string) ([]Observation, error) {
	history, _, err := s.load(key)
	return history, err
}

// SetPending records the observation for the job's newly staged archive.
func (s *Store) SetPending(key string, obs Observation) error {
	if s == nil {
		return nil
	}
	raw, err := json.Marshal(obs)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO job_anomaly (key, pending) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET pending = excluded.pending`, key, string(raw))
	return err
}

// Accept moves the staged observation into history after a successful upload.
func (s *Store) Accept(key string) error {
	if s == nil {
		return nil
	}
	history, pending, err := s.load(key)
	if err != nil || pending == nil {
		return err
	}
	history = append(history, *pending)
	if len(history) > anomaly.MaxHistory {
		history = history[len(history)-anomaly.MaxHistory:]
	}
	raw, err := json.Marshal(history)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE job_anomaly SET history = ?, pending = '' WHERE key = ?`, string(raw), key)
	return err
}

// ClearPending forgets the staged observation, for example when the staged
// archive is discarded.
func (s *Store) ClearPending(key string) error {
	if s == nil {
		return nil
	}
	_, err := s.db.Exec(`UPDATE job_anomaly SET pending = '' WHERE key = ?`, key)
	return err
}

// Delete forgets everything about a job.
func (s *Store) Delete(key string) error {
	if s == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM job_anomaly WHERE key = ?`, key)
	return err
}

func (s *Store) load(key string) ([]Observation, *Observation, error) {
	if s == nil {
		return nil, nil, nil
	}
	var historyRaw, pendingRaw string
	err := s.db.QueryRow(`SELECT history, pending FROM job_anomaly WHERE key = ?`, key).Scan(&historyRaw, &pendingRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var history []Observation
	if err := json.Unmarshal([]byte(historyRaw), &history); err != nil {
		return nil, nil, err
	}
	if pendingRaw == "" {
		return history, nil, nil
	}
	var pending Observation
	if err := json.Unmarshal([]byte(pendingRaw), &pending); err != nil {
		return nil, nil, err
	}
	return history, &pending, nil
}
