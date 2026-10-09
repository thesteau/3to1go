package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/3to1go/shared/auth"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestAutomationTokenStorage(t *testing.T) {
	s, mock := testUserStore(t)
	ctx := context.Background()
	token := auth.AutomationToken{ID: "id", Name: "automation", Scopes: []string{"read"}, CreatedAt: "2026-01-01T00:00:00Z", ExpiresAt: "2027-01-01T00:00:00Z"}
	hash := auth.AutomationTokenHash("secret")
	mock.ExpectExec("INSERT INTO app_automation_tokens").WithArgs(token.ID, 7, hash, token.Name, `["read"]`, token.CreatedAt, token.ExpiresAt).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.CreateAutomationToken(ctx, 7, hash, token); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("FROM app_automation_tokens WHERE user_id").WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "scopes", "created_at", "expires_at"}).AddRow(token.ID, token.Name, `["read"]`, token.CreatedAt, token.ExpiresAt)).RowsWillBeClosed()
	tokens, err := s.ListAutomationTokens(ctx, 7)
	if err != nil || len(tokens) != 1 || tokens[0].ID != token.ID {
		t.Fatalf("list %v %v", tokens, err)
	}
	mock.ExpectExec("DELETE FROM app_automation_tokens WHERE user_id").WithArgs(7, token.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	if revoked, err := s.RevokeAutomationToken(ctx, 7, token.ID); err != nil || !revoked {
		t.Fatal(revoked, err)
	}
	// Expired/revoked tokens must not resolve a user, even when that user exists.
	mock.ExpectQuery("FROM app_automation_tokens WHERE token_hash = .* AND expires_at >").WithArgs(hash, sqlmock.AnyArg()).WillReturnError(sql.ErrNoRows)
	user, info, err := s.UserForAutomationToken(ctx, hash)
	if err != nil || user != nil || info != nil {
		t.Fatalf("revoked lookup %v %v %v", user, info, err)
	}
}
