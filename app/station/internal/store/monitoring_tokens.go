package store

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/3to1go/station/internal/signing"
	"github.com/jackc/pgx/v5"
)

// ErrInvalidMonitoringToken means the token is malformed, expired, or revoked.
var ErrInvalidMonitoringToken = errors.New("invalid monitoring token")

// MonitoringToken is token metadata. The token itself is shown once and never stored.
type MonitoringToken struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Routes    []string `json:"routes"`
	CreatedAt string   `json:"created_at"`
	ExpiresAt string   `json:"expires_at"`
}

// MonitoringTokenStore records issued monitoring tokens, so revoking one takes effect immediately.
type MonitoringTokenStore struct {
	pool dbPool
}

func NewMonitoringTokenStore(pool dbPool) *MonitoringTokenStore {
	return &MonitoringTokenStore{pool: pool}
}

func (s *MonitoringTokenStore) EnsureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS monitoring_tokens (
			id TEXT PRIMARY KEY,
			token_hash TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			routes TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL
		)`)
	return err
}

func (s *MonitoringTokenStore) Mint(ctx context.Context, priv ed25519.PrivateKey, name string, ttlDays int, routes []string) (string, *MonitoringToken, error) {
	token, payload, err := signing.MintMonitoringToken(priv, ttlDays, routes)
	if err != nil {
		return "", nil, err
	}
	encodedRoutes, err := json.Marshal(routes)
	if err != nil {
		return "", nil, fmt.Errorf("encode monitoring token routes: %w", err)
	}
	created := time.Unix(payload.IssuedAt, 0).UTC()
	expires := time.Unix(payload.ExpiresAt, 0).UTC()
	_, err = s.pool.Exec(ctx, `
		INSERT INTO monitoring_tokens (id, token_hash, name, routes, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		payload.JTI, tokenHash(token), name, string(encodedRoutes), created, expires)
	if err != nil {
		return "", nil, err
	}
	return token, &MonitoringToken{
		ID:        payload.JTI,
		Name:      name,
		Routes:    routes,
		CreatedAt: created.Format(time.RFC3339),
		ExpiresAt: expires.Format(time.RFC3339),
	}, nil
}

// Verify returns ErrInvalidMonitoringToken for a token that can't be used, and
// another error if the database couldn't answer.
func (s *MonitoringTokenStore) Verify(ctx context.Context, token string, pub ed25519.PublicKey) (*MonitoringToken, error) {
	payload, err := signing.VerifyMonitoringToken(token, pub)
	if err != nil {
		return nil, ErrInvalidMonitoringToken
	}
	var found MonitoringToken
	var created, expires time.Time
	err = s.pool.QueryRow(ctx, `
		SELECT id, name, created_at, expires_at
		FROM monitoring_tokens
		WHERE id = $1 AND token_hash = $2 AND expires_at >= CURRENT_TIMESTAMP`,
		payload.JTI, tokenHash(token)).Scan(&found.ID, &found.Name, &created, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidMonitoringToken
	}
	if err != nil {
		return nil, err
	}
	found.Routes = payload.Routes
	found.CreatedAt = created.UTC().Format(time.RFC3339)
	found.ExpiresAt = expires.UTC().Format(time.RFC3339)
	return &found, nil
}

func (s *MonitoringTokenStore) List(ctx context.Context) ([]MonitoringToken, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, routes, created_at, expires_at FROM monitoring_tokens ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tokens := []MonitoringToken{}
	for rows.Next() {
		var token MonitoringToken
		var routes string
		var created, expires time.Time
		if err := rows.Scan(&token.ID, &token.Name, &routes, &created, &expires); err != nil {
			return nil, fmt.Errorf("list monitoring tokens: %w", err)
		}
		if err := json.Unmarshal([]byte(routes), &token.Routes); err != nil {
			return nil, fmt.Errorf("list monitoring tokens: %w", err)
		}
		token.CreatedAt = created.UTC().Format(time.RFC3339)
		token.ExpiresAt = expires.UTC().Format(time.RFC3339)
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

func (s *MonitoringTokenStore) Revoke(ctx context.Context, id string) (bool, error) {
	result, err := s.pool.Exec(ctx, `DELETE FROM monitoring_tokens WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func (s *MonitoringTokenStore) CleanupExpired(ctx context.Context) (int64, error) {
	result, err := s.pool.Exec(ctx, `DELETE FROM monitoring_tokens WHERE expires_at < CURRENT_TIMESTAMP`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
