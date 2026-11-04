package store

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/3to1go/station/internal/signing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func monitoringStoreKeys(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv, pub
}

func monitoringRow(id string) pgx.Row {
	return &mockRow{scanFn: func(dest ...any) error {
		*dest[0].(*string) = id
		*dest[1].(*string) = "prometheus"
		*dest[2].(*time.Time) = time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
		*dest[3].(*time.Time) = time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
		return nil
	}}
}

func TestMonitoringTokenMintStoresOnlyHash(t *testing.T) {
	priv, pub := monitoringStoreKeys(t)
	var args []any
	var query string
	s := NewMonitoringTokenStore(&mockPool{execFn: func(_ context.Context, sql string, a ...any) (pgconn.CommandTag, error) {
		query, args = sql, a
		return pgconn.CommandTag{}, nil
	}})
	token, info, err := s.Mint(context.Background(), priv, "prometheus", 30, []string{signing.MonitoringRouteMetrics})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := signing.VerifyMonitoringToken(token, pub)
	if err != nil || info.ID != payload.JTI || info.Name != "prometheus" {
		t.Fatalf("minted %+v, payload %+v, error %v", info, payload, err)
	}
	if !strings.Contains(query, "INSERT INTO monitoring_tokens") || args[1] != tokenHash(token) || args[3] != `["metrics"]` {
		t.Fatalf("insert: %s %v", query, args)
	}
	for _, arg := range args {
		if arg == token {
			t.Fatal("raw token was stored")
		}
	}
}

func TestMonitoringTokenMintSaveFailure(t *testing.T) {
	priv, _ := monitoringStoreKeys(t)
	s := NewMonitoringTokenStore(&mockPool{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("database down")
	}})
	if token, _, err := s.Mint(context.Background(), priv, "name", 30, []string{"report"}); err == nil || token != "" {
		t.Fatalf("token %q, error %v", token, err)
	}
}

func TestMonitoringTokenVerify(t *testing.T) {
	priv, pub := monitoringStoreKeys(t)
	token, payload, err := signing.MintMonitoringToken(priv, 30, []string{signing.MonitoringRouteReport})
	if err != nil {
		t.Fatal(err)
	}
	dbErr := errors.New("database down")

	found := NewMonitoringTokenStore(&mockPool{queryRowFn: func(_ context.Context, _ string, args ...any) pgx.Row {
		if args[0] != payload.JTI || args[1] != tokenHash(token) {
			t.Errorf("query args: %v", args)
		}
		return monitoringRow(payload.JTI)
	}})
	got, err := found.Verify(context.Background(), token, pub)
	if err != nil || !slices.Equal(got.Routes, []string{signing.MonitoringRouteReport}) || got.ExpiresAt != "2027-02-01T00:00:00Z" {
		t.Fatalf("valid token: %+v, %v", got, err)
	}

	revoked := NewMonitoringTokenStore(&mockPool{queryRowFn: func(context.Context, string, ...any) pgx.Row { return noRow() }})
	if _, err := revoked.Verify(context.Background(), token, pub); !errors.Is(err, ErrInvalidMonitoringToken) {
		t.Fatalf("revoked token: %v", err)
	}

	down := NewMonitoringTokenStore(&mockPool{queryRowFn: func(context.Context, string, ...any) pgx.Row { return errRow(dbErr) }})
	if _, err := down.Verify(context.Background(), token, pub); !errors.Is(err, dbErr) || errors.Is(err, ErrInvalidMonitoringToken) {
		t.Fatalf("database failure: %v", err)
	}

	// A bad signature is rejected without asking the database.
	unused := NewMonitoringTokenStore(&mockPool{queryRowFn: func(context.Context, string, ...any) pgx.Row {
		t.Error("queried the database for a bad token")
		return noRow()
	}})
	if _, err := unused.Verify(context.Background(), "not-a-token", pub); !errors.Is(err, ErrInvalidMonitoringToken) {
		t.Fatalf("bad token: %v", err)
	}
	station, err := signing.MintCredential(priv, 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unused.Verify(context.Background(), station, pub); !errors.Is(err, ErrInvalidMonitoringToken) {
		t.Fatalf("Station token: %v", err)
	}
}

func TestMonitoringTokenList(t *testing.T) {
	s := NewMonitoringTokenStore(&mockPool{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return &mockRows{scanFns: []func(dest ...any) error{func(dest ...any) error {
			*dest[0].(*string) = "token-id"
			*dest[1].(*string) = "prometheus"
			*dest[2].(*string) = `["metrics","report"]`
			*dest[3].(*time.Time) = time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
			*dest[4].(*time.Time) = time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
			return nil
		}}}, nil
	}})
	tokens, err := s.List(context.Background())
	if err != nil || len(tokens) != 1 || tokens[0].ID != "token-id" || len(tokens[0].Routes) != 2 || tokens[0].CreatedAt != "2026-11-03T00:00:00Z" {
		t.Fatalf("list: %+v, %v", tokens, err)
	}
	empty := NewMonitoringTokenStore(&mockPool{})
	if tokens, err := empty.List(context.Background()); err != nil || tokens == nil || len(tokens) != 0 {
		t.Fatalf("empty list: %+v, %v", tokens, err)
	}
}

func TestMonitoringTokenRevokeAndCleanup(t *testing.T) {
	var query string
	s := NewMonitoringTokenStore(&mockPool{execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
		query = sql
		return pgconn.NewCommandTag("DELETE 1"), nil
	}})
	if revoked, err := s.Revoke(context.Background(), "token-id"); err != nil || !revoked {
		t.Fatalf("revoke: %v, %v", revoked, err)
	}
	if removed, err := s.CleanupExpired(context.Background()); err != nil || removed != 1 || !strings.Contains(query, "expires_at <") {
		t.Fatalf("cleanup: %d, %v, %s", removed, err, query)
	}
	missing := NewMonitoringTokenStore(&mockPool{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("DELETE 0"), nil
	}})
	if revoked, err := missing.Revoke(context.Background(), "missing"); err != nil || revoked {
		t.Fatalf("missing: %v, %v", revoked, err)
	}
}
