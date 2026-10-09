package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/3to1go/shared/auth"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ auth.AutomationStore = (*UserStore)(nil)

func (s *UserStore) ensureAutomationSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS app_automation_tokens (
 id TEXT PRIMARY KEY,
 user_id INTEGER NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
 token_hash TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL,
 scopes TEXT NOT NULL,
 created_at TEXT NOT NULL,
 expires_at TEXT NOT NULL
 )`)
	return err
}

func (s *UserStore) CreateAutomationToken(ctx context.Context, userID int, hash string, token auth.AutomationToken) error {
	scopes, err := json.Marshal(token.Scopes)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO app_automation_tokens
 (id, user_id, token_hash, name, scopes, created_at, expires_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		token.ID, userID, hash, token.Name, string(scopes), token.CreatedAt, token.ExpiresAt)
	return err
}

func (s *UserStore) ListAutomationTokens(ctx context.Context, userID int) ([]auth.AutomationToken, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, scopes, created_at, expires_at FROM app_automation_tokens WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tokens := []auth.AutomationToken{}
	for rows.Next() {
		var token auth.AutomationToken
		var scopes string
		if err := rows.Scan(&token.ID, &token.Name, &scopes, &token.CreatedAt, &token.ExpiresAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(scopes), &token.Scopes); err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

func (s *UserStore) RevokeAutomationToken(ctx context.Context, userID int, id string) (bool, error) {
	result, err := s.pool.Exec(ctx, `DELETE FROM app_automation_tokens WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func (s *UserStore) UserForAutomationToken(ctx context.Context, hash string) (*User, *auth.AutomationToken, error) {
	if hash == "" {
		return nil, nil, nil
	}
	var token auth.AutomationToken
	var userID int
	var scopes string
	err := s.pool.QueryRow(ctx, `SELECT id, user_id, name, scopes, created_at, expires_at
 FROM app_automation_tokens WHERE token_hash = $1 AND expires_at > $2`, hash, time.Now().UTC().Format(time.RFC3339)).Scan(
		&token.ID, &userID, &token.Name, &scopes, &token.CreatedAt, &token.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal([]byte(scopes), &token.Scopes); err != nil {
		return nil, nil, err
	}
	user, err := s.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		return nil, nil, err
	}
	user, err = s.withDefaultPasswordChangeRequired(ctx, user)
	if err != nil {
		return nil, nil, err
	}
	return publicUser(user), &token, nil
}
