package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type bindingTx struct {
	pgx.Tx
	pool       *mockPool
	committed  bool
	rolledBack bool
}

func (tx *bindingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return tx.pool.Exec(ctx, sql, args...)
}
func (tx *bindingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return tx.pool.QueryRow(ctx, sql, args...)
}
func (tx *bindingTx) Commit(context.Context) error   { tx.committed = true; return nil }
func (tx *bindingTx) Rollback(context.Context) error { tx.rolledBack = true; return nil }

type bindingPool struct {
	*mockPool
	tx *bindingTx
}

func (p *bindingPool) Begin(context.Context) (pgx.Tx, error) { return p.tx, nil }

func TestCredentialBindingTransaction(t *testing.T) {
	for _, tc := range []struct {
		name, existing           string
		used, limit              int
		shared, expired, oldLive bool
		want                     error
		write                    bool
	}{
		{name: "first instance", limit: 1, write: true},
		{name: "already bound", existing: "token", used: 1, limit: 1},
		{name: "foreign binding", existing: "other", limit: 1, oldLive: true, want: ErrCredentialBinding},
		{name: "replace expired binding", existing: "other", limit: 1, write: true},
		{name: "single instance limit", used: 1, limit: 100, want: ErrCredentialLimit},
		{name: "shared available", used: 1, limit: 2, shared: true, write: true},
		{name: "shared full", used: 2, limit: 2, shared: true, want: ErrCredentialLimit},
		{name: "revoked token", expired: true, want: ErrCredentialUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lockedToken, lockedInstance, written := false, false, false
			pool := &mockPool{}
			pool.queryRowFn = func(_ context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "SELECT shared"):
					if !strings.Contains(sql, "FOR UPDATE") || args[0] != "token" {
						t.Fatal("token must be locked before registration count")
					}
					lockedToken = true
					if tc.expired {
						return noRow()
					}
					return &mockRow{scanFn: func(dest ...any) error { *dest[0].(*bool) = tc.shared; *dest[1].(*int) = tc.limit; return nil }}
				case strings.Contains(sql, "SELECT credential_hash"):
					if !lockedInstance {
						t.Fatal("instance lookup preceded lock")
					}
					if tc.existing == "" {
						return noRow()
					}
					return &mockRow{scanFn: func(dest ...any) error { *dest[0].(**string) = &tc.existing; return nil }}
				case strings.Contains(sql, "SELECT EXISTS"):
					return &mockRow{scanFn: func(dest ...any) error { *dest[0].(*bool) = tc.oldLive; return nil }}
				case strings.Contains(sql, "COUNT(*)"):
					if !lockedToken || !lockedInstance {
						t.Fatal("unlocked registration count")
					}
					return &mockRow{scanFn: func(dest ...any) error { *dest[0].(*int) = tc.used; return nil }}
				default:
					t.Fatal(sql)
					return noRow()
				}
			}
			pool.execFn = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "pg_advisory_xact_lock") {
					if !lockedToken {
						t.Fatal("token lock missing")
					}
					lockedInstance = true
				} else if strings.Contains(sql, "INSERT INTO scout_registration") {
					written = true
				} else {
					t.Fatal(sql)
				}
				return pgconn.CommandTag{}, nil
			}
			tx := &bindingTx{pool: pool}
			err := NewCredentialStore(&bindingPool{mockPool: pool, tx: tx}).Bind(context.Background(), "token", "scout", "instance")
			if !errors.Is(err, tc.want) || written != tc.write || tx.committed != (tc.want == nil) || !tx.rolledBack {
				t.Fatalf("err=%v write=%v committed=%v rollback=%v", err, written, tx.committed, tx.rolledBack)
			}
		})
	}
}
