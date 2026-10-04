package store

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRestoreDecisionScopesInstanceAndEnforcesTransitions(t *testing.T) {
	pool := &mockPool{execFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
		for _, condition := range []string{"scout_id=$1", "scout_instance_id=$2", "id=$3", "status='pending'", "status='accepted'"} {
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
