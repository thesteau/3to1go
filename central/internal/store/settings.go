package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/relay/central/internal/config"
)

type SettingsStore struct {
	pool dbPool
}

func NewSettingsStore(pool dbPool) *SettingsStore {
	return &SettingsStore{pool: pool}
}

func (s *SettingsStore) EnsureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY,
			payload JSONB NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`)
	return err
}

func (s *SettingsStore) Load(ctx context.Context) (*config.SettingsPayload, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT payload FROM app_settings WHERE key = $1`, "settings").Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var p config.SettingsPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *SettingsStore) Save(ctx context.Context, p *config.SettingsPayload) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO app_settings (key, payload, updated_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (key) DO UPDATE SET payload = EXCLUDED.payload, updated_at = CURRENT_TIMESTAMP`,
		"settings", data)
	return err
}
