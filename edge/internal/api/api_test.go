package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/3to1go/edge/internal/config"
	"github.com/3to1go/edge/internal/services/recovery"
	"github.com/3to1go/edge/internal/store"
)

// ---------------------------------------------------------------------------
// Mock userStorer
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

func (m *mockUserStore) EnsureSchema(_ context.Context) error       { return nil }
func (m *mockUserStore) EnsureDefaultAdmin(_ context.Context) error { return nil }
func (m *mockUserStore) UserForSession(_ context.Context, _ string) (*store.User, error) {
	return m.sessionUser, m.sessionErr
}
func (m *mockUserStore) Authenticate(_ context.Context, _, _ string) (*store.User, error) {
	return m.authenticateUser, m.authenticateErr
}
func (m *mockUserStore) CreateSession(_ context.Context, _ int) (string, error) {
	return m.createSessionToken, m.createSessionErr
}
func (m *mockUserStore) DeleteSession(_ context.Context, _ string) error {
	return m.deleteSessionErr
}
func (m *mockUserStore) DeleteSessionsForUser(_ context.Context, _ int) error { return nil }
func (m *mockUserStore) ListUsers(_ context.Context) ([]*store.User, error) {
	return m.listUsers, m.listErr
}
func (m *mockUserStore) GetUserByID(_ context.Context, _ int) (*store.User, error) {
	return nil, nil
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

// ---------------------------------------------------------------------------
// Mock edgeRunner
// ---------------------------------------------------------------------------

type mockRunner struct {
	settings          *config.Settings
	updateSettingsHit bool
	updateSettingsErr error
	fingerprint       string
	keyBase64         string
	statusSnapshot    map[string]any
	dirSnapshot       map[string]any
	ntfySnapshot      map[string]any
	testNtfyErr       error
	certSnapshot      map[string]any
	saveCertResult    any
	saveCertErr       error
	deleteCertErr     error
	hookSnapshot      map[string]any
	saveHookResult    any
	saveHookErr       error
	readHookName      string
	readHookContent   string
	readHookErr       error
	deleteHookErr     error
	saveJobResult     any
	saveJobErr        error
	deleteJobErr      error
	forceSendResult   map[string]any
	forceSendErr      error
	previewResult     map[string]any
	previewErr        error
	recoverResult     map[string]any
	recoverErr        error
}

func (m *mockRunner) CurrentSettings() *config.Settings { return m.settings }
func (m *mockRunner) UpdateSettings(s *config.Settings) error {
	m.updateSettingsHit = true
	if m.updateSettingsErr == nil {
		m.settings = s
	}
	return m.updateSettingsErr
}
func (m *mockRunner) EncryptionKeyFingerprint() string    { return m.fingerprint }
func (m *mockRunner) EncryptionKeyBase64() string         { return m.keyBase64 }
func (m *mockRunner) StatusSnapshot() map[string]any      { return m.statusSnapshot }
func (m *mockRunner) DirectoriesSnapshot() map[string]any { return m.dirSnapshot }
func (m *mockRunner) NtfySnapshot(_ *config.Settings) map[string]any {
	return m.ntfySnapshot
}
func (m *mockRunner) TestNtfy(_, _, _ string) error { return m.testNtfyErr }
func (m *mockRunner) CertSnapshot() map[string]any  { return m.certSnapshot }
func (m *mockRunner) SaveCertFile(_ string, _ []byte) (any, error) {
	return m.saveCertResult, m.saveCertErr
}
func (m *mockRunner) DeleteCertFile(_ string) error { return m.deleteCertErr }
func (m *mockRunner) HookSnapshot(_, _ string) map[string]any {
	return m.hookSnapshot
}
func (m *mockRunner) SaveHookFile(_ string, _ []byte) (any, error) {
	return m.saveHookResult, m.saveHookErr
}
func (m *mockRunner) ReadHookFile(_ string) (string, string, error) {
	return m.readHookName, m.readHookContent, m.readHookErr
}
func (m *mockRunner) DeleteHookFile(_ string) error { return m.deleteHookErr }
func (m *mockRunner) SaveJob(_ string, _ map[string]any) (any, error) {
	return m.saveJobResult, m.saveJobErr
}
func (m *mockRunner) DeleteJob(_ string) error { return m.deleteJobErr }
func (m *mockRunner) ForceSendJob(_ context.Context, _ string) (map[string]any, error) {
	return m.forceSendResult, m.forceSendErr
}
func (m *mockRunner) PreviewRecovery(_ context.Context, _, _ string) (map[string]any, error) {
	return m.previewResult, m.previewErr
}
func (m *mockRunner) RecoverJob(_ context.Context, _, _ string) (map[string]any, error) {
	return m.recoverResult, m.recoverErr
}

// ---------------------------------------------------------------------------
// Mock schedulerFacade
// ---------------------------------------------------------------------------

type mockScheduler struct {
	snapshot     map[string]any
	runNowResult string
	reloadErr    error
}

func (m *mockScheduler) Snapshot() map[string]any      { return m.snapshot }
func (m *mockScheduler) RequestRunNow() string         { return m.runNowResult }
func (m *mockScheduler) ReloadSettings(_ string) error { return m.reloadErr }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestApp(us userStorer) *App {
	return &App{
		runner:    nil,
		scheduler: nil,
		userStore: us,
		logger:    discardLogger(),
	}
}

func newTestAppFull(us userStorer, runner edgeRunner, sched schedulerFacade) *App {
	return &App{
		runner:    runner,
		scheduler: sched,
		userStore: us,
		logger:    discardLogger(),
	}
}

// adminUserStore returns a mockUserStore that always resolves the session to an admin user.
func adminUserStore() *mockUserStore {
	return &mockUserStore{sessionUser: &store.User{ID: 1, Username: "admin", IsAdmin: true}}
}

// regularUserStore returns a mockUserStore that always resolves the session to a non-admin user.
func regularUserStore() *mockUserStore {
	return &mockUserStore{sessionUser: &store.User{ID: 2, Username: "user", IsAdmin: false}}
}

// defaultSettings returns a zero-coerced settings object.
func defaultSettings() *config.Settings {
	s, _ := config.BuildSettings(&config.SettingsPayload{})
	return s
}

// defaultRunner returns a mockRunner with safe defaults.
func defaultRunner() *mockRunner {
	return &mockRunner{
		settings:       defaultSettings(),
		statusSnapshot: map[string]any{"edge_id": "test"},
		dirSnapshot:    map[string]any{"directories": []any{}},
		ntfySnapshot:   map[string]any{"ntfy_url": ""},
		certSnapshot:   map[string]any{"files": []any{}},
		hookSnapshot:   map[string]any{"files": []any{}},
		fingerprint:    "abc123",
		keyBase64:      "base64key",
	}
}

// defaultScheduler returns a mockScheduler with safe defaults.
func defaultScheduler() *mockScheduler {
	return &mockScheduler{
		snapshot:     map[string]any{"status": "idle"},
		runNowResult: "triggered",
	}
}

// doRequest sends a request without authentication.
func doRequest(handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

// doAuthRequest sends a request with a session cookie set.
func doAuthRequest(handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: store.SessionCookie, Value: "token"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func decodeJSON(t *testing.T, rr *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.NewDecoder(rr.Body).Decode(dst); err != nil {
		t.Fatalf("decode response JSON: %v", err)
	}
}

// buildMultipart creates a multipart/form-data body with a single file field.
func buildMultipart(t *testing.T, fieldName, filename string, content []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	fw.Write(content)
	w.Close()
	return &buf, w.FormDataContentType()
}

// ---------------------------------------------------------------------------
// GET /health
// ---------------------------------------------------------------------------

func TestHandleHealth_ReturnsOK(t *testing.T) {
	app := newTestApp(&mockUserStore{})
	rr := doRequest(app.Handler(), "GET", "/health", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var resp map[string]string
	decodeJSON(t, rr, &resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
}

// ---------------------------------------------------------------------------
// Auth middleware
// ---------------------------------------------------------------------------

func TestAuthMiddleware_ProtectedRouteRequiresLogin(t *testing.T) {
	app := newTestApp(&mockUserStore{sessionUser: nil})
	rr := doRequest(app.Handler(), "GET", "/api/status", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestAuthMiddleware_PublicPathsAllowed(t *testing.T) {
	app := newTestApp(&mockUserStore{sessionUser: nil})
	for _, path := range []string{"/api/session/login", "/api/session/me", "/api/session/logout"} {
		rr := doRequest(app.Handler(), "GET", path, nil)
		if rr.Code == http.StatusUnauthorized {
			t.Errorf("%s: got 401, public paths should not require auth", path)
		}
	}
}

func TestAuthMiddleware_AdminRouteRequiresAdmin(t *testing.T) {
	nonAdmin := &store.User{ID: 2, Username: "user", IsAdmin: false}
	app := newTestApp(&mockUserStore{sessionUser: nonAdmin})
	rr := doRequest(app.Handler(), "GET", "/api/settings", nil)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for non-admin on admin route", rr.Code)
	}
}

func TestAuthMiddleware_AdminCanAccessAdminRoute(t *testing.T) {
	newUser := &store.User{ID: 2, Username: "newbie", IsAdmin: false}
	admin := &store.User{ID: 1, Username: "admin", IsAdmin: true}
	ms := &mockUserStore{sessionUser: admin, createUserResult: newUser}
	app := newTestApp(ms)
	rr := doRequest(app.Handler(), "POST", "/api/users", map[string]any{
		"username": "newbie",
		"password": "pass",
		"is_admin": false,
	})
	if rr.Code == http.StatusUnauthorized {
		t.Error("admin should not get 401")
	}
	if rr.Code == http.StatusForbidden {
		t.Error("admin should not get 403")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestAuthMiddleware_MustChangePasswordBlocked(t *testing.T) {
	user := &store.User{ID: 2, Username: "user", IsAdmin: false, MustChangePassword: true}
	app := newTestApp(&mockUserStore{sessionUser: user})
	rr := doRequest(app.Handler(), "GET", "/api/directories", nil)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for must-change-password user", rr.Code)
	}
}

func TestAuthMiddleware_MustChangePasswordAllowedOnChangePassword(t *testing.T) {
	user := &store.User{ID: 2, Username: "user", IsAdmin: false, MustChangePassword: true}
	app := newTestApp(&mockUserStore{sessionUser: user})
	rr := doRequest(app.Handler(), "POST", "/api/session/change-password", map[string]string{})
	if rr.Code == http.StatusForbidden {
		t.Error("change-password should not be blocked by must-change-password guard")
	}
}

// ---------------------------------------------------------------------------
// GET /api/session/me
// ---------------------------------------------------------------------------

func TestHandleSessionMe_NotAuthenticated(t *testing.T) {
	app := newTestApp(&mockUserStore{sessionUser: nil})
	rr := doRequest(app.Handler(), "GET", "/api/session/me", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var resp map[string]any
	decodeJSON(t, rr, &resp)
	if resp["authenticated"] != false {
		t.Errorf("authenticated = %v, want false", resp["authenticated"])
	}
}

func TestHandleSessionMe_Authenticated(t *testing.T) {
	user := &store.User{ID: 1, Username: "alice", IsAdmin: true}
	app := newTestApp(&mockUserStore{sessionUser: user})
	rr := doRequest(app.Handler(), "GET", "/api/session/me", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var resp map[string]any
	decodeJSON(t, rr, &resp)
	if resp["authenticated"] != true {
		t.Errorf("authenticated = %v, want true", resp["authenticated"])
	}
}

// ---------------------------------------------------------------------------
// POST /api/session/login
// ---------------------------------------------------------------------------

func TestHandleLogin_Success(t *testing.T) {
	user := &store.User{ID: 1, Username: "admin", IsAdmin: true}
	ms := &mockUserStore{
		authenticateUser:   user,
		createSessionToken: "tok123",
	}
	app := newTestApp(ms)
	rr := doRequest(app.Handler(), "POST", "/api/session/login", map[string]string{
		"username": "admin",
		"password": "admin",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var found bool
	for _, c := range rr.Result().Cookies() {
		if c.Name == store.SessionCookie {
			found = true
			if c.Value != "tok123" {
				t.Errorf("cookie value = %q, want tok123", c.Value)
			}
		}
	}
	if !found {
		t.Error("expected session cookie to be set")
	}
}

func TestHandleLogin_BadCredentials(t *testing.T) {
	ms := &mockUserStore{
		authenticateUser: nil,
		authenticateErr:  errors.New("invalid credentials"),
	}
	app := newTestApp(ms)
	rr := doRequest(app.Handler(), "POST", "/api/session/login", map[string]string{
		"username": "admin",
		"password": "wrong",
	})
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestHandleLogin_InvalidBody(t *testing.T) {
	app := newTestApp(&mockUserStore{})
	req := httptest.NewRequest("POST", "/api/session/login", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	app.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHandleLogin_SessionCreationFailure(t *testing.T) {
	user := &store.User{ID: 1, Username: "admin", IsAdmin: true}
	ms := &mockUserStore{
		authenticateUser: user,
		createSessionErr: errors.New("db error"),
	}
	app := newTestApp(ms)
	rr := doRequest(app.Handler(), "POST", "/api/session/login", map[string]string{
		"username": "admin",
		"password": "admin",
	})
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/session/logout
// ---------------------------------------------------------------------------

func TestHandleLogout_ClearsSession(t *testing.T) {
	app := newTestApp(&mockUserStore{})
	req := httptest.NewRequest("POST", "/api/session/logout", nil)
	req.AddCookie(&http.Cookie{Name: store.SessionCookie, Value: "tok123"})
	rr := httptest.NewRecorder()
	app.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var cleared bool
	for _, c := range rr.Result().Cookies() {
		if c.Name == store.SessionCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("expected session cookie to be cleared (MaxAge < 0)")
	}
}

func TestHandleLogout_NoExistingCookie(t *testing.T) {
	app := newTestApp(&mockUserStore{})
	rr := doRequest(app.Handler(), "POST", "/api/session/logout", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/session/change-password
// ---------------------------------------------------------------------------

func TestHandleChangePassword_Success(t *testing.T) {
	user := &store.User{ID: 1, Username: "admin", IsAdmin: true}
	changed := &store.User{ID: 1, Username: "admin", IsAdmin: true}
	ms := &mockUserStore{sessionUser: user, changePassResult: changed}
	app := newTestApp(ms)
	rr := doAuthRequest(app.Handler(), "POST", "/api/session/change-password", map[string]string{
		"current_password":     "old",
		"new_password":         "newpass",
		"confirm_new_password": "newpass",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandleChangePassword_MismatchedPasswords(t *testing.T) {
	user := &store.User{ID: 1, Username: "admin", IsAdmin: true}
	ms := &mockUserStore{sessionUser: user}
	app := newTestApp(ms)
	rr := doAuthRequest(app.Handler(), "POST", "/api/session/change-password", map[string]string{
		"current_password":     "old",
		"new_password":         "newpass",
		"confirm_new_password": "different",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHandleChangePassword_WrongCurrentPassword(t *testing.T) {
	user := &store.User{ID: 1, Username: "admin", IsAdmin: true}
	ms := &mockUserStore{sessionUser: user, changePassErr: errors.New("wrong password")}
	app := newTestApp(ms)
	rr := doAuthRequest(app.Handler(), "POST", "/api/session/change-password", map[string]string{
		"current_password":     "wrong",
		"new_password":         "newpass",
		"confirm_new_password": "newpass",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/users
// ---------------------------------------------------------------------------

func TestHandleListUsers_AdminOnly(t *testing.T) {
	admin := &store.User{ID: 1, Username: "admin", IsAdmin: true}
	ms := &mockUserStore{
		sessionUser: admin,
		listUsers:   []*store.User{admin},
	}
	app := newTestApp(ms)
	rr := doAuthRequest(app.Handler(), "GET", "/api/users", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandleListUsers_NonAdminReturnsOwnUser(t *testing.T) {
	// Non-admins get their own user record back, not a 403.
	user := &store.User{ID: 2, Username: "user", IsAdmin: false}
	ms := &mockUserStore{sessionUser: user}
	app := newTestApp(ms)
	rr := doAuthRequest(app.Handler(), "GET", "/api/users", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var resp map[string]any
	decodeJSON(t, rr, &resp)
	if resp["users"] == nil {
		t.Error("expected 'users' key in response")
	}
}

// ---------------------------------------------------------------------------
// GET /api/status
// ---------------------------------------------------------------------------

func TestHandleStatus_ReturnsSnapshot(t *testing.T) {
	runner := defaultRunner()
	sched := defaultScheduler()
	app := newTestAppFull(regularUserStore(), runner, sched)
	rr := doAuthRequest(app.Handler(), "GET", "/api/status", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var resp map[string]any
	decodeJSON(t, rr, &resp)
	if _, ok := resp["scheduler"]; !ok {
		t.Error("response missing 'scheduler' key")
	}
	if _, ok := resp["edge_id"]; !ok {
		t.Error("response missing 'edge_id' key from status snapshot")
	}
}

func TestHandleStatus_RequiresLogin(t *testing.T) {
	app := newTestAppFull(&mockUserStore{}, defaultRunner(), defaultScheduler())
	rr := doRequest(app.Handler(), "GET", "/api/status", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/run-now
// ---------------------------------------------------------------------------

func TestHandleRunNow_Triggered(t *testing.T) {
	sched := &mockScheduler{runNowResult: "triggered", snapshot: map[string]any{}}
	app := newTestAppFull(regularUserStore(), defaultRunner(), sched)
	rr := doAuthRequest(app.Handler(), "POST", "/api/run-now", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var resp map[string]string
	decodeJSON(t, rr, &resp)
	if resp["status"] != "triggered" {
		t.Errorf("status = %q, want triggered", resp["status"])
	}
}

func TestHandleRunNow_AlreadyRunning(t *testing.T) {
	sched := &mockScheduler{runNowResult: "already_running", snapshot: map[string]any{}}
	app := newTestAppFull(regularUserStore(), defaultRunner(), sched)
	rr := doAuthRequest(app.Handler(), "POST", "/api/run-now", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var resp map[string]string
	decodeJSON(t, rr, &resp)
	if resp["status"] != "already_running" {
		t.Errorf("status = %q, want already_running", resp["status"])
	}
}

// ---------------------------------------------------------------------------
// GET /api/settings
// ---------------------------------------------------------------------------

func TestHandleGetSettings_AdminReturnsSettings(t *testing.T) {
	runner := defaultRunner()
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "GET", "/api/settings", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var resp map[string]any
	decodeJSON(t, rr, &resp)
	if _, ok := resp["settings"]; !ok {
		t.Error("response missing 'settings' key")
	}
}

func TestHandleGetSettings_NonAdminForbidden(t *testing.T) {
	app := newTestAppFull(regularUserStore(), defaultRunner(), defaultScheduler())
	rr := doAuthRequest(app.Handler(), "GET", "/api/settings", nil)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/settings
// ---------------------------------------------------------------------------

func TestHandleSaveSettings_Success(t *testing.T) {
	runner := defaultRunner()
	sched := defaultScheduler()
	app := newTestAppFull(adminUserStore(), runner, sched)
	rr := doAuthRequest(app.Handler(), "POST", "/api/settings", config.SettingsPayload{})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var resp map[string]any
	decodeJSON(t, rr, &resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
}

func TestHandleSaveSettings_InvalidBody(t *testing.T) {
	app := newTestAppFull(adminUserStore(), defaultRunner(), defaultScheduler())
	req := httptest.NewRequest("POST", "/api/settings", strings.NewReader("bad-json"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: store.SessionCookie, Value: "token"})
	rr := httptest.NewRecorder()
	app.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHandleSaveSettings_InvalidCronDoesNotSave(t *testing.T) {
	runner := defaultRunner()
	originalCron := runner.settings.CronSchedule
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/settings", config.SettingsPayload{
		CronSchedule: "0 2 * * 8",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
	if runner.updateSettingsHit {
		t.Error("UpdateSettings was called for invalid cron")
	}
	if runner.settings.CronSchedule != originalCron {
		t.Errorf("cron schedule changed to %q, want %q", runner.settings.CronSchedule, originalCron)
	}
	var resp map[string]any
	decodeJSON(t, rr, &resp)
	if detail, _ := resp["detail"].(string); !strings.Contains(detail, "cron_schedule") {
		t.Errorf("detail = %q, want cron_schedule error", detail)
	}
}

func TestHandleSaveSettings_UpdateFailure(t *testing.T) {
	runner := &mockRunner{
		settings:          defaultSettings(),
		updateSettingsErr: errors.New("cycle running"),
	}
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/settings", config.SettingsPayload{})
	if rr.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/ntfy
// ---------------------------------------------------------------------------

func TestHandleGetNtfy_ReturnsSnapshot(t *testing.T) {
	runner := defaultRunner()
	runner.ntfySnapshot = map[string]any{"ntfy_url": "https://ntfy.example.com"}
	app := newTestAppFull(regularUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "GET", "/api/ntfy", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var resp map[string]any
	decodeJSON(t, rr, &resp)
	if resp["ntfy_url"] != "https://ntfy.example.com" {
		t.Errorf("ntfy_url = %v, want https://ntfy.example.com", resp["ntfy_url"])
	}
}

// ---------------------------------------------------------------------------
// POST /api/ntfy
// ---------------------------------------------------------------------------

func TestHandleSaveNtfy_Success(t *testing.T) {
	runner := defaultRunner()
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/ntfy", map[string]string{
		"ntfy_url":   "https://ntfy.example.com",
		"ntfy_topic": "backups",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandleSaveNtfy_UpdateFailure(t *testing.T) {
	runner := &mockRunner{
		settings:          defaultSettings(),
		updateSettingsErr: errors.New("cycle running"),
		ntfySnapshot:      map[string]any{},
	}
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/ntfy", map[string]string{})
	if rr.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/ntfy/test
// ---------------------------------------------------------------------------

func TestHandleTestNtfy_Success(t *testing.T) {
	runner := defaultRunner()
	app := newTestAppFull(regularUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/ntfy/test", map[string]string{
		"ntfy_url":   "https://ntfy.example.com",
		"ntfy_topic": "backups",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandleTestNtfy_Failure(t *testing.T) {
	runner := defaultRunner()
	runner.testNtfyErr = errors.New("connection refused")
	app := newTestAppFull(regularUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/ntfy/test", map[string]string{
		"ntfy_url": "https://ntfy.example.com",
	})
	if rr.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/certificates
// ---------------------------------------------------------------------------

func TestHandleGetCertificates_ReturnsSnapshot(t *testing.T) {
	runner := defaultRunner()
	runner.certSnapshot = map[string]any{"files": []any{}}
	app := newTestAppFull(regularUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "GET", "/api/certificates", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/certificates/files
// ---------------------------------------------------------------------------

func TestHandleUploadCertificate_MissingFile(t *testing.T) {
	app := newTestAppFull(adminUserStore(), defaultRunner(), defaultScheduler())
	req := httptest.NewRequest("POST", "/api/certificates/files", nil)
	req.AddCookie(&http.Cookie{Name: store.SessionCookie, Value: "token"})
	rr := httptest.NewRecorder()
	app.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHandleUploadCertificate_Success(t *testing.T) {
	runner := defaultRunner()
	runner.saveCertResult = map[string]any{"name": "cert.pem"}
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())

	body, ct := buildMultipart(t, "certificate_file", "cert.pem", []byte("---CERT---"))
	req := httptest.NewRequest("POST", "/api/certificates/files", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(&http.Cookie{Name: store.SessionCookie, Value: "token"})
	rr := httptest.NewRecorder()
	app.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandleUploadCertificate_SaveError(t *testing.T) {
	runner := defaultRunner()
	runner.saveCertErr = errors.New("invalid cert")
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())

	body, ct := buildMultipart(t, "certificate_file", "bad.pem", []byte("not-a-cert"))
	req := httptest.NewRequest("POST", "/api/certificates/files", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(&http.Cookie{Name: store.SessionCookie, Value: "token"})
	rr := httptest.NewRecorder()
	app.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// DELETE /api/certificates/files/{filename}
// ---------------------------------------------------------------------------

func TestHandleDeleteCertificate_Success(t *testing.T) {
	app := newTestAppFull(adminUserStore(), defaultRunner(), defaultScheduler())
	rr := doAuthRequest(app.Handler(), "DELETE", "/api/certificates/files/cert.pem", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandleDeleteCertificate_NotFound(t *testing.T) {
	runner := defaultRunner()
	runner.deleteCertErr = errors.New("cert.pem: not found")
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "DELETE", "/api/certificates/files/cert.pem", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestHandleDeleteCertificate_OtherError(t *testing.T) {
	runner := defaultRunner()
	runner.deleteCertErr = errors.New("permission denied")
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "DELETE", "/api/certificates/files/cert.pem", nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/hooks
// ---------------------------------------------------------------------------

func TestHandleGetHooks_ReturnsSnapshot(t *testing.T) {
	runner := defaultRunner()
	runner.hookSnapshot = map[string]any{"pre_command": "", "post_command": ""}
	app := newTestAppFull(regularUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "GET", "/api/hooks", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/hooks
// ---------------------------------------------------------------------------

func TestHandleSaveHooks_Success(t *testing.T) {
	runner := defaultRunner()
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/hooks", map[string]string{
		"hook_pre_command": "echo pre",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandleSaveHooks_UpdateFailure(t *testing.T) {
	runner := &mockRunner{
		settings:          defaultSettings(),
		updateSettingsErr: errors.New("cycle running"),
		hookSnapshot:      map[string]any{},
	}
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/hooks", map[string]string{})
	if rr.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/hooks/files
// ---------------------------------------------------------------------------

func TestHandleUploadHookFile_MissingFile(t *testing.T) {
	app := newTestAppFull(adminUserStore(), defaultRunner(), defaultScheduler())
	req := httptest.NewRequest("POST", "/api/hooks/files", nil)
	req.AddCookie(&http.Cookie{Name: store.SessionCookie, Value: "token"})
	rr := httptest.NewRecorder()
	app.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHandleUploadHookFile_Success(t *testing.T) {
	runner := defaultRunner()
	runner.saveHookResult = map[string]any{"name": "pre.sh"}
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())

	body, ct := buildMultipart(t, "hook_file", "pre.sh", []byte("#!/bin/sh\necho hello"))
	req := httptest.NewRequest("POST", "/api/hooks/files", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(&http.Cookie{Name: store.SessionCookie, Value: "token"})
	rr := httptest.NewRecorder()
	app.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/hooks/files/{filename}
// ---------------------------------------------------------------------------

func TestHandleViewHookFile_Success(t *testing.T) {
	runner := defaultRunner()
	runner.readHookName = "pre.sh"
	runner.readHookContent = "#!/bin/sh"
	app := newTestAppFull(regularUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "GET", "/api/hooks/files/pre.sh", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var resp map[string]string
	decodeJSON(t, rr, &resp)
	if resp["filename"] != "pre.sh" {
		t.Errorf("filename = %q, want pre.sh", resp["filename"])
	}
}

func TestHandleViewHookFile_NotFound(t *testing.T) {
	runner := defaultRunner()
	runner.readHookErr = fmt.Errorf("pre.sh: not found")
	app := newTestAppFull(regularUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "GET", "/api/hooks/files/pre.sh", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// DELETE /api/hooks/files/{filename}
// ---------------------------------------------------------------------------

func TestHandleDeleteHookFile_Success(t *testing.T) {
	app := newTestAppFull(adminUserStore(), defaultRunner(), defaultScheduler())
	rr := doAuthRequest(app.Handler(), "DELETE", "/api/hooks/files/pre.sh", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandleDeleteHookFile_NotFound(t *testing.T) {
	runner := defaultRunner()
	runner.deleteHookErr = errors.New("pre.sh: not found")
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "DELETE", "/api/hooks/files/pre.sh", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/encryption-key
// ---------------------------------------------------------------------------

func TestHandleGetEncryptionKey_AdminReturnsKey(t *testing.T) {
	runner := defaultRunner()
	runner.fingerprint = "deadbeef"
	runner.keyBase64 = "AAAA"
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "GET", "/api/encryption-key", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var resp map[string]string
	decodeJSON(t, rr, &resp)
	if resp["fingerprint"] != "deadbeef" {
		t.Errorf("fingerprint = %q, want deadbeef", resp["fingerprint"])
	}
	if resp["key_base64"] != "AAAA" {
		t.Errorf("key_base64 = %q, want AAAA", resp["key_base64"])
	}
}

func TestHandleGetEncryptionKey_NonAdminForbidden(t *testing.T) {
	app := newTestAppFull(regularUserStore(), defaultRunner(), defaultScheduler())
	rr := doAuthRequest(app.Handler(), "GET", "/api/encryption-key", nil)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/directories
// ---------------------------------------------------------------------------

func TestHandleListDirectories_ReturnsSnapshot(t *testing.T) {
	runner := defaultRunner()
	runner.dirSnapshot = map[string]any{
		"scan_root":   "/data",
		"directories": []any{},
	}
	app := newTestAppFull(regularUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "GET", "/api/directories", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var resp map[string]any
	decodeJSON(t, rr, &resp)
	if resp["scan_root"] != "/data" {
		t.Errorf("scan_root = %v, want /data", resp["scan_root"])
	}
}

// ---------------------------------------------------------------------------
// POST /api/directories/save-job
// ---------------------------------------------------------------------------

func TestHandleSaveJob_Success(t *testing.T) {
	runner := defaultRunner()
	runner.saveJobResult = map[string]any{"relative_path": "photos"}
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/directories/save-job", map[string]any{
		"relative_path": "photos",
		"config":        map[string]any{"job_name": "photos"},
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var resp map[string]any
	decodeJSON(t, rr, &resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %v, want ok", resp["status"])
	}
}

func TestHandleSaveJob_Failure(t *testing.T) {
	runner := defaultRunner()
	runner.saveJobErr = errors.New("invalid path")
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/directories/save-job", map[string]any{
		"relative_path": "../escape",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHandleSaveJob_InvalidBody(t *testing.T) {
	app := newTestAppFull(adminUserStore(), defaultRunner(), defaultScheduler())
	req := httptest.NewRequest("POST", "/api/directories/save-job", strings.NewReader("bad"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: store.SessionCookie, Value: "token"})
	rr := httptest.NewRecorder()
	app.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/directories/delete-job
// ---------------------------------------------------------------------------

func TestHandleDeleteJob_Success(t *testing.T) {
	app := newTestAppFull(adminUserStore(), defaultRunner(), defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/directories/delete-job", map[string]string{
		"relative_path": "photos",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandleDeleteJob_Failure(t *testing.T) {
	runner := defaultRunner()
	runner.deleteJobErr = errors.New("path not found")
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/directories/delete-job", map[string]string{
		"relative_path": "missing",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/directories/force-send
// ---------------------------------------------------------------------------

func TestHandleForceSend_Success(t *testing.T) {
	runner := defaultRunner()
	runner.forceSendResult = map[string]any{"status": "started", "job_name": "photos"}
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/directories/force-send", map[string]string{
		"job_name": "photos",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandleForceSend_NotFound(t *testing.T) {
	runner := defaultRunner()
	runner.forceSendErr = errors.New("job not found")
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/directories/force-send", map[string]string{
		"job_name": "ghost",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/recovery/preview
// ---------------------------------------------------------------------------

func TestHandleRecoveryPreview_Success(t *testing.T) {
	runner := defaultRunner()
	runner.previewResult = map[string]any{"status": "ok", "entries": []any{}}
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/recovery/preview", map[string]string{
		"relative_path": "photos",
		"fingerprint":   "abc123",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandleRecoveryPreview_RecoveryError(t *testing.T) {
	runner := defaultRunner()
	runner.previewErr = &recovery.RecoveryError{Message: "wrong key", StatusCode: http.StatusConflict}
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/recovery/preview", map[string]string{
		"relative_path": "photos",
		"fingerprint":   "abc",
	})
	if rr.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rr.Code)
	}
}

func TestHandleRecoveryPreview_GenericError(t *testing.T) {
	runner := defaultRunner()
	runner.previewErr = errors.New("unexpected failure")
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/recovery/preview", map[string]string{
		"relative_path": "photos",
		"fingerprint":   "abc",
	})
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/recovery/restore
// ---------------------------------------------------------------------------

func TestHandleRecoveryRestore_Success(t *testing.T) {
	runner := defaultRunner()
	runner.recoverResult = map[string]any{"status": "restored"}
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/recovery/restore", map[string]string{
		"relative_path": "photos",
		"fingerprint":   "abc123",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandleRecoveryRestore_RecoveryError(t *testing.T) {
	runner := defaultRunner()
	runner.recoverErr = &recovery.RecoveryError{Message: "wrong key", StatusCode: http.StatusConflict}
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/recovery/restore", map[string]string{
		"relative_path": "photos",
		"fingerprint":   "abc",
	})
	if rr.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rr.Code)
	}
}

func TestHandleRecoveryRestore_GenericError(t *testing.T) {
	runner := defaultRunner()
	runner.recoverErr = errors.New("disk full")
	app := newTestAppFull(adminUserStore(), runner, defaultScheduler())
	rr := doAuthRequest(app.Handler(), "POST", "/api/recovery/restore", map[string]string{
		"relative_path": "photos",
		"fingerprint":   "abc",
	})
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// isPublicPath
// ---------------------------------------------------------------------------

func TestIsPublicPath(t *testing.T) {
	public := []string{
		"/api/session/me",
		"/api/session/login",
		"/api/session/logout",
		"/api/session/change-password",
		"/",
		"/static/app.js",
		"/static/app.css",
		"/health",
	}
	for _, p := range public {
		if !isPublicPath(p) {
			t.Errorf("expected %q to be public", p)
		}
	}
}

func TestIsPublicPath_Protected(t *testing.T) {
	protected := []string{
		"/api/status",
		"/api/settings",
		"/api/directories",
		"/api/users",
	}
	for _, p := range protected {
		if isPublicPath(p) {
			t.Errorf("expected %q to be protected", p)
		}
	}
}
