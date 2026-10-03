package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/3to1go/scout/internal/config"
)

// SettingsStore persists the scout settings payload in SQLite.
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

// legacySettingsKeys maps setting keys saved by an older release to their current names.
var legacySettingsKeys = map[string]string{
	"edge_id":         "scout_id",
	"central_url":     "station_url",
	"edge_credential": "scout_credential",
}

// MigrateLegacyKeys renames setting keys saved by an older release. A key that already has its
// current name keeps its value. It does nothing once no old keys remain.
func (s *SettingsStore) MigrateLegacyKeys(ctx context.Context) error {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM app_settings WHERE id = 1`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return err
	}
	changed := false
	for old, current := range legacySettingsKeys {
		value, ok := payload[old]
		if !ok {
			continue
		}
		if _, exists := payload[current]; !exists {
			payload[current] = value
		}
		delete(payload, old)
		changed = true
	}
	if !changed {
		return nil
	}
	migrated, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE app_settings SET payload = ? WHERE id = 1`, string(migrated))
	return err
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
