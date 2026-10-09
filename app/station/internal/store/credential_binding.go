package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrCredentialUnavailable = errors.New("Station token is expired or revoked")
	ErrCredentialBinding     = errors.New("Station token is not bound to this Scout instance")
	ErrCredentialLimit       = errors.New("Station token registration limit reached")
)

type transactionPool interface {
	Begin(context.Context) (pgx.Tx, error)
}

// Bind reserves the instance before ingestion. The token row lock serializes
// registration counts across Station processes; the instance lock also prevents
// two different tokens from claiming the same previously unseen instance.
func (s *CredentialStore) Bind(ctx context.Context, hash, scoutID, instanceID string) error {
	pool, ok := s.pool.(transactionPool)
	if !ok {
		return errors.New("Station token store does not support transactions")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var shared bool
	var limit int
	err = tx.QueryRow(ctx, `SELECT shared, max_registrations FROM scout_credentials
		WHERE token_hash = $1 AND expires_at >= CURRENT_TIMESTAMP FOR UPDATE`, hash).Scan(&shared, &limit)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCredentialUnavailable
	}
	if err != nil {
		return err
	}
	if !shared {
		limit = 1
	}
	limit = max(limit, 1)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "credential-instance:"+scoutID+"/"+instanceID); err != nil {
		return err
	}
	var existing *string
	err = tx.QueryRow(ctx, `SELECT credential_hash FROM scout_registration
		WHERE scout_id = $1 AND scout_instance_id = $2 FOR UPDATE`, scoutID, instanceID).Scan(&existing)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if existing != nil && *existing != "" {
		if *existing == hash {
			return tx.Commit(ctx)
		}
		// Expired or revoked bindings may be replaced, including after an
		// interrupted cleanup. A live token must never be displaced.
		var live bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scout_credentials WHERE token_hash = $1 AND expires_at >= CURRENT_TIMESTAMP)`, *existing).Scan(&live); err != nil {
			return err
		}
		if live {
			return ErrCredentialBinding
		}
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM scout_registration WHERE credential_hash = $1`, hash).Scan(&count); err != nil {
		return err
	}
	if count >= limit {
		return ErrCredentialLimit
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = tx.Exec(ctx, `INSERT INTO scout_registration
		(scout_id, scout_instance_id, first_seen_at, last_seen_at, credential_hash)
		VALUES ($1, $2, $3, $3, $4)
		ON CONFLICT (scout_id, scout_instance_id) DO UPDATE SET credential_hash = EXCLUDED.credential_hash`, scoutID, instanceID, now, hash)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type CredentialInfo struct {
	TokenHash        string `json:"token_hash"`
	ExpiresAt        string `json:"expires_at"`
	CreatedAt        string `json:"created_at"`
	Shared           bool   `json:"shared"`
	MaxRegistrations int    `json:"max_registrations"`
}

func (s *CredentialStore) List(ctx context.Context) ([]CredentialInfo, error) {
	rows, err := s.pool.Query(ctx, `SELECT token_hash, expires_at, created_at, shared, max_registrations
		FROM scout_credentials ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CredentialInfo{}
	for rows.Next() {
		var item CredentialInfo
		var expires, created time.Time
		if err := rows.Scan(&item.TokenHash, &expires, &created, &item.Shared, &item.MaxRegistrations); err != nil {
			return nil, fmt.Errorf("list Station tokens: %w", err)
		}
		item.ExpiresAt = expires.UTC().Format(time.RFC3339)
		item.CreatedAt = created.UTC().Format(time.RFC3339)
		items = append(items, item)
	}
	return items, rows.Err()
}
