package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/3to1go/edge/internal/config"
)

// SettingsStore persists the edge settings payload in SQLite.
type SettingsStore struct {
	db *sql.DB
}

func NewSettingsStore(db *sql.DB) *SettingsStore {
	return &SettingsStore{db: db}
}

func (s *SettingsStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS app_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			payload TEXT NOT NULL
		)`)
	return err
}

// Load returns the stored SettingsPayload, or nil if not yet saved.
func (s *SettingsStore) Load(ctx context.Context) (*config.SettingsPayload, error) {
	row := s.db.QueryRowContext(ctx, `SELECT payload FROM app_settings WHERE id = 1`)
	var raw string
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var p config.SettingsPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Save persists the payload, replacing any previous row.
func (s *SettingsStore) Save(ctx context.Context, payload *config.SettingsPayload) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO app_settings (id, payload) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET payload = excluded.payload`, string(raw))
	return err
}
