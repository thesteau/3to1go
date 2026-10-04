package store

import (
	"context"
	"strings"
	"testing"

	"github.com/3to1go/shared/protocol"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRestoreDecisionScopesInstanceAndEnforcesTransitions(t *testing.T) {
	pool := &mockPool{execFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
		for _, condition := range []string{"scout_id=$1", "scout_instance_id=$2", "id=$3", "status='pending'", "status='accepted'", restoreRequestLive} {
			if !strings.Contains(query, condition) {
				t.Errorf("missing condition %s", condition)
			}
		}
		if args[0] != "scout" || args[1] != "instance" || args[2] != "request" || args[3] != "rejected" {
			t.Errorf("args: %v", args)
		}
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}}
	changed, err := NewSnapshotIndex(pool).DecideRestoreRequest(context.Background(), "scout", "instance", "request", "rejected")
	if err != nil || changed {
		t.Fatalf("changed %v, error %v", changed, err)
	}
}

func TestRestoreRequestsExpireAfterOneHour(t *testing.T) {
	if !strings.Contains(restoreRequestLive, "interval '1 hour'") {
		t.Fatalf("lifetime is %q", restoreRequestLive)
	}
	var deletes []string
	var listQuery string
	pool := &mockPool{
		execFn: func(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
			if strings.HasPrefix(query, "DELETE") {
				deletes = append(deletes, query)
			}
			return pgconn.NewCommandTag("DELETE 0"), nil
		},
		queryFn: func(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
			listQuery = query
			return emptyRows(), nil
		},
	}
	index := NewSnapshotIndex(pool)
	if err := index.CreateRestoreRequest(context.Background(), protocol.RestoreRequest{ID: "request"}); err != nil {
		t.Fatal(err)
	}
	if _, err := index.ListRestoreRequests(context.Background(), "scout", "instance"); err != nil {
		t.Fatal(err)
	}
	if len(deletes) != 2 {
		t.Fatalf("expected expired requests to be deleted on create and list, got %v", deletes)
	}
	for _, query := range deletes {
		if !strings.Contains(query, "NOT ("+restoreRequestLive+")") || strings.Contains(query, "status") {
			t.Errorf("prune must remove every status once expired: %s", query)
		}
	}
	if !strings.Contains(listQuery, restoreRequestLive) {
		t.Errorf("list must hide expired requests: %s", listQuery)
	}
}
