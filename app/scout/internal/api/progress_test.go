package api

import (
	"encoding/json"
	"testing"

	"github.com/3to1go/scout/internal/services/recovery"
)

func TestRecoveryProgressRequiresAdminAndFiltersOperation(t *testing.T) {
	percent := 42
	runner := &mockRunner{recoveryProgress: recovery.Progress{OperationID: "current", Status: "running", Phase: "downloading", Percent: &percent, BytesDone: 42, BytesTotal: 100}}
	app := newTestAppFull(adminUserStore(), runner, nil)
	for _, id := range []string{"current", "previous"} {
		rr := doAuthRequest(app.Handler(), "GET", "/api/recovery/progress?operation_id="+id, nil)
		if rr.Code != 200 || rr.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("progress response: %d %s", rr.Code, rr.Body.String())
		}
		var progress recovery.Progress
		if err := json.Unmarshal(rr.Body.Bytes(), &progress); err != nil {
			t.Fatal(err)
		}
		if id == "current" && (progress.Status != "running" || progress.Percent == nil || *progress.Percent != 42) {
			t.Fatalf("progress: %+v", progress)
		}
		if id == "previous" && (progress.Status != "idle" || progress.OperationID != "") {
			t.Fatalf("leaked a previous restore: %+v", progress)
		}
	}
	app = newTestAppFull(regularUserStore(), runner, nil)
	if rr := doAuthRequest(app.Handler(), "GET", "/api/recovery/progress", nil); rr.Code != 403 {
		t.Fatalf("non-admin: %d", rr.Code)
	}
	app = newTestAppFull(&mockUserStore{}, runner, nil)
	if rr := doRequest(app.Handler(), "GET", "/api/recovery/progress", nil); rr.Code != 401 {
		t.Fatalf("anonymous: %d", rr.Code)
	}
}
