package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/3to1go/shared/protocol"
	"github.com/3to1go/station/internal/store"
)

func (m *mockSnapIndex) CreateRestoreRequest(context.Context, protocol.RestoreRequest) error {
	return nil
}
func (m *mockSnapIndex) ListRestoreRequests(context.Context, string, string) ([]protocol.RestoreRequest, error) {
	return []protocol.RestoreRequest{}, nil
}
func (m *mockSnapIndex) DecideRestoreRequest(context.Context, string, string, string, string) (bool, error) {
	return true, nil
}

type restoreIndex struct {
	*mockSnapIndex
	requests []protocol.RestoreRequest
	decision string
}

func (m *restoreIndex) CreateRestoreRequest(_ context.Context, r protocol.RestoreRequest) error {
	m.requests = append(m.requests, r)
	return nil
}
func (m *restoreIndex) ListRestoreRequests(context.Context, string, string) ([]protocol.RestoreRequest, error) {
	return m.requests, nil
}
func (m *restoreIndex) DecideRestoreRequest(_ context.Context, _, _, _, status string) (bool, error) {
	m.decision = status
	return true, nil
}

func TestRequestRestoreSavesExactArchiveAndNotifiesScout(t *testing.T) {
	var notified string
	scout := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backup/restore-notifications" || r.Method != "POST" {
			t.Errorf("unexpected notification: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		notified = body["id"]
		w.WriteHeader(200)
	}))
	defer scout.Close()
	fingerprint, advertisedURL := "key-fingerprint", scout.URL
	index := &restoreIndex{mockSnapIndex: &mockSnapIndex{getReg: &store.ScoutRegistration{EncryptionKeyFingerprint: &fingerprint, AdvertisedURL: &advertisedURL}}}
	app := newTestApp(t, nil, nil, nil, index)
	filename := "photos__2026-10-03T00-00-00Z__abcdef12.tar.zst"
	dir := filepath.Join(app.Settings().BackupRoot, "scout", "instance", "photos")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	req := withUser(jsonReq("POST", "/", nil), adminUser())
	for key, value := range map[string]string{"scout_id": "scout", "scout_instance_id": "instance", "job_name": "photos", "filename": filename} {
		req.SetPathValue(key, value)
	}
	rr := httptest.NewRecorder()
	app.handleRequestRestore(rr, req)
	if rr.Code != 201 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if len(index.requests) != 1 || index.requests[0].Filename != filename || index.requests[0].ID != notified {
		t.Fatalf("wrong request: %+v, notified %s", index.requests, notified)
	}
}

func TestRequestRestoreRequiresKeyAndAdmin(t *testing.T) {
	for _, test := range []struct {
		name   string
		user   *store.User
		status int
	}{{"no key", adminUser(), 409}, {"not admin", regularUser(), 403}} {
		t.Run(test.name, func(t *testing.T) {
			app := newTestApp(t, nil, nil, nil, &mockSnapIndex{getReg: &store.ScoutRegistration{}})
			req := withUser(jsonReq("POST", "/", nil), test.user)
			for key, value := range map[string]string{"scout_id": "scout", "scout_instance_id": "instance", "job_name": "photos", "filename": "snapshot.tar.zst"} {
				req.SetPathValue(key, value)
			}
			rr := httptest.NewRecorder()
			app.handleRequestRestore(rr, req)
			if rr.Code != test.status {
				t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestRestoreRequestEndpointsRejectOtherInstanceCredential(t *testing.T) {
	hash := "bound-credential"
	app := newTestApp(t, nil, &mockCredStore{verifyResult: &store.CredentialRecord{TokenHash: "other-credential"}}, nil, &mockSnapIndex{getReg: &store.ScoutRegistration{CredentialHash: &hash}})
	for _, path := range []string{"/backup/recovery/scout/instance/requests", "/backup/recovery/scout/instance/photos/archive/snapshot.tar.zst"} {
		req := jsonReq("GET", path, nil)
		req.Header.Set("Authorization", "Bearer token")
		rr := httptest.NewRecorder()
		app.Handler().ServeHTTP(rr, req)
		if rr.Code != 403 {
			t.Fatalf("%s: status %d: %s", path, rr.Code, rr.Body.String())
		}
	}
}
