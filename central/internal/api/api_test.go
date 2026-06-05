package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3to1go/central/internal/config"
	"github.com/3to1go/central/internal/ingest"
	"github.com/3to1go/central/internal/services"
	"github.com/3to1go/central/internal/storage"
	"github.com/3to1go/central/internal/store"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

type mockUserStore struct {
	sessionUser        *store.User
	sessionErr         error
	authenticateUser   *store.User
	authenticateErr    error
	createSessionToken string
	createSessionErr   error
	deleteSessionErr   error
	listUsers          []*store.User
	listErr            error
	createUserResult   *store.User
	createUserErr      error
	updateUserResult   *store.User
	updateUserErr      error
	deleteUserErr      error
	changePassResult   *store.User
	changePassErr      error
}

func (m *mockUserStore) UserForSession(_ context.Context, _ string) (*store.User, error) {
	return m.sessionUser, m.sessionErr
}
func (m *mockUserStore) Authenticate(_ context.Context, _, _ string) (*store.User, error) {
	return m.authenticateUser, m.authenticateErr
}
func (m *mockUserStore) CreateSession(_ context.Context, _ int) (string, error) {
	return m.createSessionToken, m.createSessionErr
}
func (m *mockUserStore) DeleteSession(_ context.Context, _ string) error      { return m.deleteSessionErr }
func (m *mockUserStore) DeleteSessionsForUser(_ context.Context, _ int) error { return nil }
func (m *mockUserStore) ListUsers(_ context.Context) ([]*store.User, error) {
	return m.listUsers, m.listErr
}
func (m *mockUserStore) CreateUser(_ context.Context, _, _ string, _ bool) (*store.User, error) {
	return m.createUserResult, m.createUserErr
}
func (m *mockUserStore) UpdateUser(_ context.Context, _ int, _, _ *string, _, _ *bool) (*store.User, error) {
	return m.updateUserResult, m.updateUserErr
}
func (m *mockUserStore) DeleteUser(_ context.Context, _ int) error { return m.deleteUserErr }
func (m *mockUserStore) ChangePassword(_ context.Context, _ int, _, _ string) (*store.User, error) {
	return m.changePassResult, m.changePassErr
}

type mockCredStore struct {
	verifyResult *store.CredentialRecord
	verifyErr    error
	mintResult   string
	mintErr      error
	revokeN      int64
	revokeErr    error
	cleanupN     int64
	cleanupErr   error
}

func (m *mockCredStore) Verify(_ context.Context, _ string, _ ed25519.PublicKey) (*store.CredentialRecord, error) {
	return m.verifyResult, m.verifyErr
}
func (m *mockCredStore) Mint(_ context.Context, _ ed25519.PrivateKey, _ int) (string, error) {
	return m.mintResult, m.mintErr
}
func (m *mockCredStore) Revoke(_ context.Context, _ string) (int64, error) {
	return m.revokeN, m.revokeErr
}
func (m *mockCredStore) CleanupExpired(_ context.Context) (int64, error) {
	return m.cleanupN, m.cleanupErr
}

type mockSettingsStore struct{ saveErr error }

func (m *mockSettingsStore) Save(_ context.Context, _ *config.SettingsPayload) error {
	return m.saveErr
}

type mockSnapIndex struct {
	getReg      *store.EdgeRegistration
	getRegErr   error
	deleteErr   error
	hasEntries  bool
	hasErr      error
	upsertErr   error
	listRegs    []store.EdgeRegistration
	listRegsErr error
	listNS      []store.NamespaceEntry
	listNSErr   error
}

func (m *mockSnapIndex) GetEdgeRegistration(_ context.Context, _, _ string) (*store.EdgeRegistration, error) {
	return m.getReg, m.getRegErr
}
func (m *mockSnapIndex) DeleteEdgeRegistration(_ context.Context, _, _ string) error {
	return m.deleteErr
}
func (m *mockSnapIndex) HasNamespaceEntries(_ context.Context, _, _ string) (bool, error) {
	return m.hasEntries, m.hasErr
}
func (m *mockSnapIndex) UpsertEdgeRegistration(_ context.Context, _ *store.EdgeRegistration) error {
	return m.upsertErr
}
func (m *mockSnapIndex) ListEdgeRegistrations(_ context.Context, _ *string) ([]store.EdgeRegistration, error) {
	return m.listRegs, m.listRegsErr
}
func (m *mockSnapIndex) ListNamespaces(_ context.Context) ([]store.NamespaceEntry, error) {
	return m.listNS, m.listNSErr
}

type mockIngest struct {
	startResp *ingest.SessionResponse
	startErr  error
	chunkResp *ingest.ChunkResponse
	chunkErr  error
	finResp   *ingest.FinalizeResponse
	finErr    error
}

func (m *mockIngest) StartUpload(_ context.Context, _ ingest.UploadInitRequest, _, _ *string) (*ingest.SessionResponse, error) {
	return m.startResp, m.startErr
}
func (m *mockIngest) AppendChunk(_ context.Context, _ string, _ int64, _ io.Reader) (*ingest.ChunkResponse, error) {
	return m.chunkResp, m.chunkErr
}
func (m *mockIngest) FinalizeUpload(_ context.Context, _ string) (*ingest.FinalizeResponse, error) {
	return m.finResp, m.finErr
}
func (m *mockIngest) ReconcileNamespace(_ context.Context, _ string) {}
func (m *mockIngest) CleanupLoop(_ context.Context, _ int)           {}
func (m *mockIngest) UpdateSettings(_ *config.Settings)              {}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestApp(t *testing.T, us userStorer, cs credStorer, ss settingsStorer, si snapIndexer) *App {
	t.Helper()
	t.Setenv("ISSUER_KEY_FILE", filepath.Join(t.TempDir(), "issuer.key"))
	t.Setenv("INDEX_DATABASE_URL", "postgresql://user:pass@localhost/test")
	settings, err := config.BuildSettings(nil)
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	settings.BackupRoot = t.TempDir()
	settings.StagingDir = t.TempDir()
	backend := storage.NewLocalBackend(settings.BackupRoot)
	hooks := services.NewHookManager(t.TempDir(), discardLogger())
	certs := services.NewCertManager(t.TempDir())
	ntfy := services.NewNtfyPublisher(discardLogger())
	if us == nil {
		us = &mockUserStore{}
	}
	if cs == nil {
		cs = &mockCredStore{}
	}
	if ss == nil {
		ss = &mockSettingsStore{}
	}
	if si == nil {
		si = &mockSnapIndex{}
	}
	return NewApp(settings, us, cs, ss, si, backend, &mockIngest{}, hooks, certs, ntfy, discardLogger())
}

func jsonReq(method, path string, body interface{}) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func withUser(r *http.Request, u *store.User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), contextKeyUser, u))
}

func adminUser() *store.User {
	return &store.User{ID: 1, Username: "admin", IsAdmin: true}
}

func regularUser() *store.User {
	return &store.User{ID: 2, Username: "bob", IsAdmin: false}
}

// ---------------------------------------------------------------------------
// Pure function tests
// ---------------------------------------------------------------------------

func TestIsPublicPath(t *testing.T) {
	public := []string{
		"/", "/health", "/health/ready",
		"/static/app.js",
		"/api/session/me", "/api/session/login", "/api/session/logout", "/api/session/change-password",
		"/backup/uploads/abc", "/backup/recovery/edge/inst/job",
	}
	for _, p := range public {
		if !isPublicPath(p) {
			t.Errorf("isPublicPath(%q) = false, want true", p)
		}
	}
	private := []string{"/api/overview", "/api/users", "/api/certificates"}
	for _, p := range private {
		if isPublicPath(p) {
			t.Errorf("isPublicPath(%q) = true, want false", p)
		}
	}
}

func TestCurrentUser_NoContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	if u := currentUser(req); u != nil {
		t.Errorf("currentUser without context value should be nil, got %v", u)
	}
}

func TestCurrentUser_WithUser(t *testing.T) {
	req := withUser(httptest.NewRequest("GET", "/", nil), adminUser())
	u := currentUser(req)
	if u == nil || u.Username != "admin" {
		t.Errorf("currentUser = %v, want admin user", u)
	}
}

func TestIsNotFoundError(t *testing.T) {
	if !isNotFoundError(errors.New("file not found")) {
		t.Error("expected true for 'not found' error")
	}
	if isNotFoundError(errors.New("some other error")) {
		t.Error("expected false for non-not-found error")
	}
	if isNotFoundError(nil) {
		t.Error("expected false for nil error")
	}
}

func TestIsRuntimeError(t *testing.T) {
	execErr := &services.ExecError{Err: errors.New("exit 1")}
	if !isRuntimeError(execErr) {
		t.Error("expected true for ExecError")
	}
	if isRuntimeError(errors.New("regular error")) {
		t.Error("expected false for regular error")
	}
}

func TestValidatedNamespace_Valid(t *testing.T) {
	ns, err := validatedNamespace("edge1", "inst1", "job1")
	if err != nil || ns != "edge1/inst1/job1" {
		t.Errorf("validatedNamespace = %q, %v", ns, err)
	}
}

func TestValidatedNamespace_InvalidEdge(t *testing.T) {
	_, err := validatedNamespace("bad edge!", "inst", "job")
	if err == nil {
		t.Error("expected error for invalid edge_id")
	}
}

func TestValidatedLegacyNamespace(t *testing.T) {
	ns, err := validatedLegacyNamespace("edge1", "job1")
	if err != nil || ns != "edge1/job1" {
		t.Errorf("validatedLegacyNamespace = %q, %v", ns, err)
	}
}

func TestWriteHTTPError_HTTPError(t *testing.T) {
	rr := httptest.NewRecorder()
	writeHTTPError(rr, &ingest.HTTPError{Code: 409, Message: "conflict"})
	if rr.Code != 409 {
		t.Errorf("code = %d, want 409", rr.Code)
	}
}

func TestWriteHTTPError_GenericError(t *testing.T) {
	rr := httptest.NewRecorder()
	writeHTTPError(rr, errors.New("something broke"))
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Session middleware
// ---------------------------------------------------------------------------

func TestSessionMiddleware_PublicPath_NoUser(t *testing.T) {
	app := newTestApp(t, &mockUserStore{sessionUser: nil}, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	app.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("health: code = %d, want 200", rr.Code)
	}
}

func TestSessionMiddleware_ProtectedPath_NoUser(t *testing.T) {
	app := newTestApp(t, &mockUserStore{sessionUser: nil}, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/overview", nil)
	app.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("overview without auth: code = %d, want 401", rr.Code)
	}
}

func TestSessionMiddleware_ProtectedPath_MustChangePassword(t *testing.T) {
	user := &store.User{ID: 1, IsAdmin: true, MustChangePassword: true}
	app := newTestApp(t, &mockUserStore{sessionUser: user}, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/overview", nil)
	req.AddCookie(&http.Cookie{Name: store.SessionCookie, Value: "token"})
	app.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("must-change-password: code = %d, want 403", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// handleHealth / handleHealthReady
// ---------------------------------------------------------------------------

func TestHandleHealth(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	app.handleHealth(rr, httptest.NewRequest("GET", "/health", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("handleHealth code = %d, want 200", rr.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("health status = %v, want ok", resp["status"])
	}
}

func TestHandleHealthReady(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	app.handleHealthReady(rr, httptest.NewRequest("GET", "/health/ready", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("handleHealthReady code = %d, want 200", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// handleSessionMe
// ---------------------------------------------------------------------------

func TestHandleSessionMe_NotAuthenticated(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	app.handleSessionMe(rr, httptest.NewRequest("GET", "/api/session/me", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("code = %d", rr.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["authenticated"] != false {
		t.Errorf("authenticated = %v, want false", resp["authenticated"])
	}
}

func TestHandleSessionMe_Authenticated(t *testing.T) {
	app := newTestApp(t, &mockUserStore{sessionUser: adminUser()}, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/session/me", nil)
	req.AddCookie(&http.Cookie{Name: store.SessionCookie, Value: "validtoken"})
	app.handleSessionMe(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("code = %d", rr.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["authenticated"] != true {
		t.Errorf("authenticated = %v, want true", resp["authenticated"])
	}
}

// ---------------------------------------------------------------------------
// handleLogin / handleLogout
// ---------------------------------------------------------------------------

func TestHandleLogin_InvalidJSON(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/session/login", strings.NewReader("notjson"))
	app.handleLogin(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rr.Code)
	}
}

func TestHandleLogin_InvalidCredentials(t *testing.T) {
	app := newTestApp(t, &mockUserStore{authenticateUser: nil}, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := jsonReq("POST", "/api/session/login", map[string]string{"username": "x", "password": "y"})
	app.handleLogin(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rr.Code)
	}
}

func TestHandleLogin_Success(t *testing.T) {
	app := newTestApp(t, &mockUserStore{
		authenticateUser:   adminUser(),
		createSessionToken: "tok123",
	}, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := jsonReq("POST", "/api/session/login", map[string]string{"username": "admin", "password": "admin"})
	app.handleLogin(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", rr.Code)
	}
}

func TestHandleLogin_CreateSessionError(t *testing.T) {
	app := newTestApp(t, &mockUserStore{
		authenticateUser: adminUser(),
		createSessionErr: errors.New("db error"),
	}, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := jsonReq("POST", "/api/session/login", map[string]string{"username": "admin", "password": "pass"})
	app.handleLogin(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", rr.Code)
	}
}

func TestHandleLogout(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/session/logout", nil)
	req.AddCookie(&http.Cookie{Name: store.SessionCookie, Value: "sometoken"})
	app.handleLogout(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// handleChangePassword
// ---------------------------------------------------------------------------

func TestHandleChangePassword_NotLoggedIn(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	app.handleChangePassword(rr, httptest.NewRequest("POST", "/api/session/change-password", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rr.Code)
	}
}

func TestHandleChangePassword_PasswordMismatch(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := jsonReq("POST", "/api/session/change-password", map[string]string{
		"current_password":     "old",
		"new_password":         "new1",
		"confirm_new_password": "new2",
	})
	req = withUser(req, regularUser())
	app.handleChangePassword(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rr.Code)
	}
}

func TestHandleChangePassword_Success(t *testing.T) {
	updated := &store.User{ID: 2, Username: "bob"}
	app := newTestApp(t, &mockUserStore{changePassResult: updated}, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := jsonReq("POST", "/api/session/change-password", map[string]string{
		"current_password":     "oldpass",
		"new_password":         "newpass",
		"confirm_new_password": "newpass",
	})
	req = withUser(req, regularUser())
	app.handleChangePassword(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// handleListUsers
// ---------------------------------------------------------------------------

func TestHandleListUsers_RequiresAuth(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	app.handleListUsers(rr, httptest.NewRequest("GET", "/api/users", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rr.Code)
	}
}

func TestHandleListUsers_Admin(t *testing.T) {
	users := []*store.User{adminUser(), regularUser()}
	app := newTestApp(t, &mockUserStore{listUsers: users}, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := withUser(httptest.NewRequest("GET", "/api/users", nil), adminUser())
	app.handleListUsers(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", rr.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	if usersArr, ok := resp["users"].([]interface{}); !ok || len(usersArr) != 2 {
		t.Errorf("expected 2 users, got %v", resp["users"])
	}
}

func TestHandleListUsers_RegularUser_SeesSelf(t *testing.T) {
	u := regularUser()
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := withUser(httptest.NewRequest("GET", "/api/users", nil), u)
	app.handleListUsers(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// handleCreateUser
// ---------------------------------------------------------------------------

func TestHandleCreateUser_RequiresAdmin(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := jsonReq("POST", "/api/users", map[string]interface{}{"username": "new", "password": "pass123", "is_admin": false})
	req = withUser(req, regularUser())
	app.handleCreateUser(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("code = %d, want 403", rr.Code)
	}
}

func TestHandleCreateUser_ShortPassword(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := jsonReq("POST", "/api/users", map[string]interface{}{"username": "new", "password": "abc"})
	req = withUser(req, adminUser())
	app.handleCreateUser(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rr.Code)
	}
}

func TestHandleCreateUser_Success(t *testing.T) {
	created := &store.User{ID: 5, Username: "newuser"}
	app := newTestApp(t, &mockUserStore{createUserResult: created}, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := jsonReq("POST", "/api/users", map[string]interface{}{"username": "newuser", "password": "secret123"})
	req = withUser(req, adminUser())
	app.handleCreateUser(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// handleDeleteUser
// ---------------------------------------------------------------------------

func TestHandleDeleteUser_RequiresAdmin(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := withUser(httptest.NewRequest("DELETE", "/api/users/2", nil), regularUser())
	req.SetPathValue("user_id", "2")
	app.handleDeleteUser(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("code = %d, want 403", rr.Code)
	}
}

func TestHandleDeleteUser_InvalidID(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := withUser(httptest.NewRequest("DELETE", "/api/users/bad", nil), adminUser())
	req.SetPathValue("user_id", "bad")
	app.handleDeleteUser(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rr.Code)
	}
}

func TestHandleDeleteUser_Success(t *testing.T) {
	app := newTestApp(t, &mockUserStore{}, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := withUser(httptest.NewRequest("DELETE", "/api/users/5", nil), adminUser())
	req.SetPathValue("user_id", "5")
	app.handleDeleteUser(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// handleUpdateUser
// ---------------------------------------------------------------------------

func TestHandleUpdateUser_NonAdminChangingOther(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := jsonReq("PUT", "/api/users/99", map[string]string{"username": "new"})
	req = withUser(req, regularUser()) // ID=2, not 99
	req.SetPathValue("user_id", "99")
	app.handleUpdateUser(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("code = %d, want 403", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// handleOverview
// ---------------------------------------------------------------------------

func TestHandleOverview_RequiresAuth(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	app.handleOverview(rr, httptest.NewRequest("GET", "/api/overview", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rr.Code)
	}
}

func TestHandleOverview_Success(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, &mockSnapIndex{})
	rr := httptest.NewRecorder()
	req := withUser(httptest.NewRequest("GET", "/api/overview", nil), adminUser())
	app.handleOverview(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", rr.Code)
	}
}

func TestHandleOverview_SnapIndexError(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, &mockSnapIndex{listRegsErr: errors.New("db down")})
	rr := httptest.NewRecorder()
	req := withUser(httptest.NewRequest("GET", "/api/overview", nil), adminUser())
	app.handleOverview(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// handleSaveSettings
// ---------------------------------------------------------------------------

func TestHandleSaveSettings_RequiresAuth(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	app.handleSaveSettings(rr, httptest.NewRequest("POST", "/api/settings", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rr.Code)
	}
}

func TestHandleSaveSettings_InvalidJSON(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/settings", strings.NewReader("notjson"))
	req = withUser(req, adminUser())
	app.handleSaveSettings(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rr.Code)
	}
}

func TestHandleSaveSettings_SaveError(t *testing.T) {
	app := newTestApp(t, nil, nil, &mockSettingsStore{saveErr: errors.New("db error")}, &mockSnapIndex{})
	rr := httptest.NewRecorder()
	req := jsonReq("POST", "/api/settings", config.SettingsPayload{})
	req = withUser(req, adminUser())
	app.handleSaveSettings(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// handleGetNtfy / handleGetHooks / handleGetCertificates
// ---------------------------------------------------------------------------

func TestHandleGetNtfy_RequiresAuth(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	app.handleGetNtfy(rr, httptest.NewRequest("GET", "/api/ntfy", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rr.Code)
	}
}

func TestHandleGetNtfy_Success(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := withUser(httptest.NewRequest("GET", "/api/ntfy", nil), adminUser())
	app.handleGetNtfy(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", rr.Code)
	}
}

func TestHandleGetHooks_Success(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := withUser(httptest.NewRequest("GET", "/api/hooks", nil), adminUser())
	app.handleGetHooks(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", rr.Code)
	}
}

func TestHandleGetCertificates_RequiresAdmin(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := withUser(httptest.NewRequest("GET", "/api/certificates", nil), regularUser())
	app.handleGetCertificates(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("code = %d, want 403", rr.Code)
	}
}

func TestHandleGetCertificates_Success(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := withUser(httptest.NewRequest("GET", "/api/certificates", nil), adminUser())
	app.handleGetCertificates(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// handleInitiateUpload / handleAppendChunk / handleFinalizeUpload
// ---------------------------------------------------------------------------

func TestHandleInitiateUpload_NoAuth(t *testing.T) {
	app := newTestApp(t, nil, &mockCredStore{verifyErr: errors.New("unauthorized")}, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/backup/uploads/initiate", nil)
	app.handleInitiateUpload(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rr.Code)
	}
}

func TestHandleAppendChunk_NoAuth(t *testing.T) {
	app := newTestApp(t, nil, &mockCredStore{verifyErr: errors.New("unauthorized")}, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/backup/uploads/abc/chunk?offset=0", nil)
	req.SetPathValue("upload_id", "abc")
	app.handleAppendChunk(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rr.Code)
	}
}

func TestHandleAppendChunk_InvalidOffset(t *testing.T) {
	cred := &store.CredentialRecord{TokenHash: "h"}
	app := newTestApp(t, nil, &mockCredStore{verifyResult: cred}, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/backup/uploads/abc/chunk?offset=notanumber", nil)
	req.Header.Set("Authorization", "Bearer sometoken")
	req.SetPathValue("upload_id", "abc")
	app.handleAppendChunk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rr.Code)
	}
}

func TestHandleFinalizeUpload_NoAuth(t *testing.T) {
	app := newTestApp(t, nil, &mockCredStore{verifyErr: errors.New("unauthorized")}, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/backup/uploads/abc/finalize", nil)
	req.SetPathValue("upload_id", "abc")
	app.handleFinalizeUpload(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// handleRevokeCredential
// ---------------------------------------------------------------------------

func TestHandleRevokeCredential_RequiresAdmin(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	req := withUser(httptest.NewRequest("DELETE", "/api/credentials/instances/e/i", nil), regularUser())
	req.SetPathValue("edge_id", "edge1")
	req.SetPathValue("edge_instance_id", "inst1")
	app.handleRevokeCredential(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("code = %d, want 403", rr.Code)
	}
}

func TestHandleRevokeCredential_InstanceNotFound(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, &mockSnapIndex{getReg: nil})
	rr := httptest.NewRecorder()
	req := withUser(httptest.NewRequest("DELETE", "/api/credentials/instances/edge1/inst1", nil), adminUser())
	req.SetPathValue("edge_id", "edge1")
	req.SetPathValue("edge_instance_id", "inst1")
	app.handleRevokeCredential(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", rr.Code)
	}
}

func TestHandleRevokeCredential_NoCredentialHash(t *testing.T) {
	reg := &store.EdgeRegistration{EdgeID: "edge1", EdgeInstanceID: "inst1"}
	app := newTestApp(t, nil, nil, nil, &mockSnapIndex{getReg: reg})
	rr := httptest.NewRecorder()
	req := withUser(httptest.NewRequest("DELETE", "/api/credentials/instances/edge1/inst1", nil), adminUser())
	req.SetPathValue("edge_id", "edge1")
	req.SetPathValue("edge_instance_id", "inst1")
	app.handleRevokeCredential(rr, req)
	if rr.Code != http.StatusConflict {
		t.Errorf("code = %d, want 409", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// App lifecycle
// ---------------------------------------------------------------------------

func TestApp_ApplySettings(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	s2, _ := config.BuildSettings(nil)
	s2.RetentionKeepLast = 99
	app.ApplySettings(s2)
	if app.Settings().RetentionKeepLast != 99 {
		t.Error("ApplySettings did not update settings")
	}
}

func TestApp_Shutdown(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	app.RestartCleanupLoop(3600)
	app.Shutdown() // should not panic
}
