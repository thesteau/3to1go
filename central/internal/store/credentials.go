package store

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/relay/central/internal/signing"
)

type CredentialStore struct {
	pool dbPool
}

func NewCredentialStore(pool dbPool) *CredentialStore {
	return &CredentialStore{pool: pool}
}

func (s *CredentialStore) EnsureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS edge_credentials (
			token_hash TEXT PRIMARY KEY,
			jti TEXT NOT NULL UNIQUE,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS idx_edge_credentials_expires_at
		ON edge_credentials (expires_at)`)
	return err
}

func (s *CredentialStore) Mint(ctx context.Context, priv ed25519.PrivateKey, ttlDays int) (string, error) {
	token, err := signing.MintCredential(priv, ttlDays)
	if err != nil {
		return "", fmt.Errorf("mint credential: %w", err)
	}
	payload, err := signing.DecodeCredentialPayload(token)
	if err != nil {
		return "", err
	}
	expiresAt := time.Unix(payload.ExpiresAt, 0).UTC()
	tokenHash := tokenHash(token)

	_, err = s.pool.Exec(ctx, `
		INSERT INTO edge_credentials (token_hash, jti, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (token_hash) DO UPDATE SET jti = EXCLUDED.jti, expires_at = EXCLUDED.expires_at`,
		tokenHash, payload.JTI, expiresAt)
	if err != nil {
		return "", err
	}
	return token, nil
}

type CredentialRecord struct {
	TokenHash string
	JTI       string
	ExpiresAt string
}

func (s *CredentialStore) Verify(ctx context.Context, token string, pub ed25519.PublicKey) (*CredentialRecord, error) {
	payload, err := signing.VerifyCredential(token, pub)
	if err != nil {
		return nil, err
	}
	hash := tokenHash(token)

	var rec CredentialRecord
	var expiresAt time.Time
	err = s.pool.QueryRow(ctx, `
		SELECT token_hash, jti, expires_at
		FROM edge_credentials
		WHERE token_hash = $1 AND jti = $2 AND expires_at >= CURRENT_TIMESTAMP`,
		hash, payload.JTI).Scan(&rec.TokenHash, &rec.JTI, &expiresAt)
	if err != nil {
		return nil, errors.New("credential revoked")
	}
	rec.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)
	return &rec, nil
}

func (s *CredentialStore) Revoke(ctx context.Context, tokenHash string) (int64, error) {
	if tokenHash == "" {
		return 0, nil
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM edge_credentials WHERE token_hash = $1`, tokenHash)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *CredentialStore) CleanupExpired(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM edge_credentials WHERE expires_at < CURRENT_TIMESTAMP`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
