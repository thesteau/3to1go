package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestMigrateLegacyNamesRenamesOldObjects(t *testing.T) {
	var statements []string
	pool := &mockPool{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		statements = append(statements, sql)
		return pgconn.CommandTag{}, nil
	}}
	if err := MigrateLegacyNames(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	if len(statements) != 1 {
		t.Fatalf("statements = %d, want 1", len(statements))
	}
	for _, want := range []string{
		"ALTER TABLE edge_credentials RENAME TO scout_credentials",
		"ALTER TABLE edge_registration RENAME TO scout_registration",
		"replace(c.column_name, 'edge_', 'scout_')",
		"'idx_edge_registration_instance', 'idx_scout_registration_instance'",
		"to_regclass('scout_credentials') IS NULL",
		"UPDATE app_settings",
		"UPDATE upload_sessions",
	} {
		if !strings.Contains(statements[0], want) {
			t.Errorf("migration is missing %q", want)
		}
	}
}

func TestMigrateLegacyNamesReturnsErrors(t *testing.T) {
	pool := &mockPool{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("exec error")
	}}
	if err := MigrateLegacyNames(context.Background(), pool); err == nil {
		t.Fatal("expected error")
	}
}
