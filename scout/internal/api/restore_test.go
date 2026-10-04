package api

import (
	"context"
	"testing"

	"github.com/3to1go/shared/protocol"
)

func (m *mockRunner) RestoreRequests(context.Context) ([]protocol.RestoreRequest, error) {
	return []protocol.RestoreRequest{}, nil
}
func (m *mockRunner) DecideRestoreRequest(context.Context, string, string, string) (any, error) {
	return map[string]string{"status": "rejected"}, nil
}

type restoreMockRunner struct {
	*mockRunner
	requests []protocol.RestoreRequest
	decided  bool
}

func (m *restoreMockRunner) RestoreRequests(context.Context) ([]protocol.RestoreRequest, error) {
	return m.requests, nil
}
func (m *restoreMockRunner) DecideRestoreRequest(context.Context, string, string, string) (any, error) {
	m.decided = true
	return map[string]string{"status": "rejected"}, nil
}

func TestRestoreNotificationVerifiesRequestWithoutRestoring(t *testing.T) {
	runner := &restoreMockRunner{mockRunner: &mockRunner{}, requests: []protocol.RestoreRequest{{ID: "known"}}}
	app := newTestAppFull(&mockUserStore{}, runner, nil)
	for _, test := range []struct {
		id     string
		status int
	}{{"known", 200}, {"untrusted", 404}} {
		rr := doRequest(app.Handler(), "POST", "/backup/restore-notifications", map[string]string{"id": test.id})
		if rr.Code != test.status {
			t.Fatalf("%s: status %d: %s", test.id, rr.Code, rr.Body.String())
		}
	}
	if runner.decided {
		t.Fatal("notification must not accept or restore")
	}
}
