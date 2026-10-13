package api

import (
	"context"
	"errors"
	"testing"

	"github.com/3to1go/scout/internal/services/state"
)

type uploadApprovalsMockRunner struct {
	*mockRunner
	approvals []state.UploadApproval
	err       error
}

func (m *uploadApprovalsMockRunner) LargeUploadApprovals(context.Context) ([]state.UploadApproval, error) {
	return m.approvals, m.err
}

func TestUploadApprovalsRequireAdmin(t *testing.T) {
	for _, tc := range []struct {
		name  string
		users userStorer
		want  int
	}{
		{"anonymous", &mockUserStore{}, 401}, {"regular", regularUserStore(), 403}, {"admin", adminUserStore(), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestAppFull(tc.users, defaultRunner(), defaultScheduler())
			for _, method := range []string{"GET", "DELETE"} {
				path := "/api/upload-approvals"
				if method == "DELETE" {
					path += "/id"
				}
				rr := doAuthRequest(app.Handler(), method, path, nil)
				if rr.Code != tc.want {
					t.Fatalf("%s: %d %s", method, rr.Code, rr.Body.String())
				}
			}
		})
	}
}

func TestUploadApprovalsListAndRemove(t *testing.T) {
	runner := &uploadApprovalsMockRunner{mockRunner: defaultRunner(), approvals: []state.UploadApproval{
		{ID: "approval-id", JobName: "docs", JobPath: "/scan/docs", StationURL: "https://station.example", ApprovedAt: "2026-10-13T00:00:00Z"},
	}}
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "GET", "/api/upload-approvals", nil)
	if rr.Code != 200 {
		t.Fatalf("list: %d %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Approvals []state.UploadApproval `json:"approvals"`
	}
	decodeJSON(t, rr, &body)
	if len(body.Approvals) != 1 || body.Approvals[0].ID != "approval-id" {
		t.Fatalf("list: %+v", body)
	}
	rr = doAuthRequest(app.Handler(), "DELETE", "/api/upload-approvals/approval-id", nil)
	if rr.Code != 200 || runner.pathActionPath != "approval-id" {
		t.Fatalf("remove: %d %s", rr.Code, rr.Body.String())
	}
	runner.err = errors.New("load failed")
	if rr := doAuthRequest(app.Handler(), "GET", "/api/upload-approvals", nil); rr.Code != 500 {
		t.Fatalf("load failure: %d", rr.Code)
	}
	runner.pathActionErr = errors.New("a backup operation is running")
	if rr := doAuthRequest(app.Handler(), "DELETE", "/api/upload-approvals/approval-id", nil); rr.Code != 409 {
		t.Fatalf("busy removal: %d", rr.Code)
	}
}
