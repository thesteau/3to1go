package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testSigningKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func signingKeyFunc(key ed25519.PrivateKey) func() (ed25519.PrivateKey, error) {
	return func() (ed25519.PrivateKey, error) { return key, nil }
}

func testAutomationToken(t *testing.T, key ed25519.PrivateKey, scopes ...string) string {
	t.Helper()
	now := time.Now()
	token, err := mintAutomationToken(key, 1, "token-id", scopes, now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestAutomationTokenClaims(t *testing.T) {
	key := testSigningKey(t)
	token := testAutomationToken(t, key, ScopeReport)
	if err := verifyAutomationToken(token, key.Public().(ed25519.PublicKey)); err != nil {
		t.Fatal(err)
	}
	claims := &automationClaims{}
	parsed, _, err := jwt.NewParser().ParseUnverified(token, claims)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Method.Alg() != "EdDSA" || claims.Subject != "1" || claims.ID != "token-id" || len(claims.Scopes) != 1 || claims.Scopes[0] != ScopeReport || claims.Audience[0] != AutomationAudience {
		t.Fatalf("token: %v %+v", parsed.Method.Alg(), claims)
	}
}

func TestVerifyAutomationTokenRejects(t *testing.T) {
	key := testSigningKey(t)
	pub := key.Public().(ed25519.PublicKey)
	now := time.Now()
	valid := jwt.RegisteredClaims{
		Audience:  jwt.ClaimStrings{AutomationAudience},
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		ID:        "token-id",
	}
	with := func(change func(*jwt.RegisteredClaims)) automationClaims {
		claims := valid
		change(&claims)
		return automationClaims{RegisteredClaims: claims}
	}
	sign := func(method jwt.SigningMethod, claims automationClaims, signingKey any) string {
		t.Helper()
		token, err := jwt.NewWithClaims(method, claims).SignedString(signingKey)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	same := func(*jwt.RegisteredClaims) {}
	good := sign(jwt.SigningMethodEdDSA, with(same), key)
	parts := strings.Split(good, ".")
	otherPayload := strings.Split(sign(jwt.SigningMethodEdDSA, with(func(c *jwt.RegisteredClaims) { c.ID = "other" }), key), ".")[1]

	if err := verifyAutomationToken(good, pub); err != nil {
		t.Fatalf("control token rejected: %v", err)
	}
	for name, token := range map[string]string{
		"unsigned":                 sign(jwt.SigningMethodNone, with(same), jwt.UnsafeAllowNoneSignatureType),
		"public key as HMAC":       sign(jwt.SigningMethodHS256, with(same), []byte(pub)),
		"signature removed":        parts[0] + "." + parts[1] + ".",
		"payload swapped":          parts[0] + "." + otherPayload + "." + parts[2],
		"another key":              sign(jwt.SigningMethodEdDSA, with(same), testSigningKey(t)),
		"expired":                  sign(jwt.SigningMethodEdDSA, with(func(c *jwt.RegisteredClaims) { c.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Hour)) }), key),
		"no expiry":                sign(jwt.SigningMethodEdDSA, with(func(c *jwt.RegisteredClaims) { c.ExpiresAt = nil }), key),
		"other audience":           sign(jwt.SigningMethodEdDSA, with(func(c *jwt.RegisteredClaims) { c.Audience = jwt.ClaimStrings{"other"} }), key),
		"no audience, Scout token": sign(jwt.SigningMethodEdDSA, with(func(c *jwt.RegisteredClaims) { c.Audience = nil }), key),
		"no ID":                    sign(jwt.SigningMethodEdDSA, with(func(c *jwt.RegisteredClaims) { c.ID = "" }), key),
		"old token format":         "3to1go_api_secret",
		"empty":                    "",
	} {
		t.Run(name, func(t *testing.T) {
			if err := verifyAutomationToken(token, pub); err == nil {
				t.Fatal("token accepted")
			}
		})
	}
}

func TestRequireRouteScope(t *testing.T) {
	key := testSigningKey(t)
	admin := &User{ID: 1, IsAdmin: true}
	metricsToken := &AutomationToken{Scopes: []string{ScopeMetrics}}
	good := "Bearer " + testAutomationToken(t, key, ScopeMetrics)
	for _, tc := range []struct {
		name   string
		header string
		user   *User
		token  *AutomationToken
		want   int
	}{
		{"allowed", good, admin, metricsToken, http.StatusOK},
		{"no header", "", admin, metricsToken, http.StatusUnauthorized},
		{"not bearer", "Basic x", admin, metricsToken, http.StatusUnauthorized},
		{"old token format", "Bearer 3to1go_api_secret", admin, metricsToken, http.StatusUnauthorized},
		{"bad signature", good + "x", admin, metricsToken, http.StatusUnauthorized},
		{"revoked", good, nil, nil, http.StatusUnauthorized},
		{"owner lost admin", good, &User{ID: 1}, metricsToken, http.StatusUnauthorized},
		{"owner must change password", good, &User{ID: 1, IsAdmin: true, MustChangePassword: true}, metricsToken, http.StatusUnauthorized},
		{"other route", good, admin, &AutomationToken{Scopes: []string{ScopeReport}}, http.StatusForbidden},
		{"manage is not enough", good, admin, &AutomationToken{Scopes: []string{"manage"}}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &automationStoreMock{sessionStore: &sessionStore{}, user: tc.user, info: tc.token}
			h := &Handler{Store: s, SigningKey: signingKeyFunc(key)}
			handler := h.RequireRouteScope(ScopeMetrics, func() bool { return true }, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			r := httptest.NewRequest("GET", "/metrics", nil)
			r.Header.Set("Authorization", tc.header)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if tc.want == http.StatusUnauthorized && w.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatal("missing WWW-Authenticate")
			}
		})
	}
}

func TestRequireRouteScopeSkippedWhenNotRequired(t *testing.T) {
	h := &Handler{}
	handler := h.RequireRouteScope(ScopeMetrics, func() bool { return false }, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != http.StatusTeapot {
		t.Fatal(w.Code)
	}
}

type failingAutomationStore struct {
	*automationStoreMock
}

func (failingAutomationStore) UserForAutomationToken(context.Context, string) (*User, *AutomationToken, error) {
	return nil, nil, errors.New("private database details")
}

func TestRequireRouteScopeHidesFailures(t *testing.T) {
	key := testSigningKey(t)
	token := "Bearer " + testAutomationToken(t, key, ScopeMetrics)
	for name, h := range map[string]*Handler{
		"store": {Store: failingAutomationStore{&automationStoreMock{sessionStore: &sessionStore{}}}, SigningKey: signingKeyFunc(key)},
		"key": {Store: &automationStoreMock{sessionStore: &sessionStore{}}, SigningKey: func() (ed25519.PrivateKey, error) {
			return nil, errors.New("private key path")
		}},
	} {
		t.Run(name, func(t *testing.T) {
			handler := h.RequireRouteScope(ScopeMetrics, func() bool { return true }, http.NotFoundHandler())
			r := httptest.NewRequest("GET", "/metrics", nil)
			r.Header.Set("Authorization", token)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "private") {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestRouteScopesGrantNoAPIAccess(t *testing.T) {
	key := testSigningKey(t)
	token := testAutomationToken(t, key, ScopeMetrics, ScopeReport)
	for _, path := range []string{"/api/status", "/api/overview", "/api/settings", "/api/encryption-key", "/api/directories"} {
		s := &automationStoreMock{sessionStore: &sessionStore{}, user: &User{ID: 1, IsAdmin: true}, info: &AutomationToken{Scopes: []string{ScopeMetrics, ScopeReport}}}
		h := &Handler{Store: s, CookieName: "session", SigningKey: signingKeyFunc(key)}
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Errorf("%s reached the handler", path)
		}), func(string) bool { return false }).ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
}

func TestUnsignedBearerRejectedOnAPI(t *testing.T) {
	key := testSigningKey(t)
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, automationClaims{RegisteredClaims: jwt.RegisteredClaims{
		Audience:  jwt.ClaimStrings{AutomationAudience},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		ID:        "token-id",
	}}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	s := &automationStoreMock{sessionStore: &sessionStore{}, user: &User{ID: 1, IsAdmin: true}, info: &AutomationToken{Scopes: []string{"manage"}}}
	h := &Handler{Store: s, CookieName: "session", SigningKey: signingKeyFunc(key)}
	r := httptest.NewRequest("GET", "/api/settings", nil)
	r.Header.Set("Authorization", "Bearer "+unsigned)
	w := httptest.NewRecorder()
	h.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("unsigned token reached the handler")
	}), func(string) bool { return false }).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || s.hash != "" {
		t.Fatalf("status %d, looked up %q", w.Code, s.hash)
	}
}

func createAutomationToken(t *testing.T, h *Handler, s *automationStoreMock, scopes string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/automation-tokens", strings.NewReader(`{"name":"nightly","scopes":`+scopes+`}`))
	r = r.WithContext(context.WithValue(r.Context(), ContextKeyUser, s.user))
	w := httptest.NewRecorder()
	h.CreateAutomationToken(w, r)
	return w
}

func TestCreateAutomationTokenSignsAndLimitsScopes(t *testing.T) {
	key := testSigningKey(t)
	s := &automationStoreMock{sessionStore: &sessionStore{}, user: &User{ID: 7, IsAdmin: true}}

	h := &Handler{Store: s, SigningKey: signingKeyFunc(key), RouteScopes: []string{ScopeMetrics, ScopeReport}}
	w := createAutomationToken(t, h, s, `["read","backup","restore","manage","metrics","report"]`)
	if w.Code != http.StatusCreated {
		t.Fatalf("all scopes: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if err := verifyAutomationToken(body.Token, key.Public().(ed25519.PublicKey)); err != nil {
		t.Fatalf("issued token: %v", err)
	}
	if s.createdHash != AutomationTokenHash(body.Token) || s.info.ID == "" {
		t.Fatalf("stored %q, info %+v", s.createdHash, s.info)
	}

	metricsOnly := &Handler{Store: s, SigningKey: signingKeyFunc(key), RouteScopes: []string{ScopeMetrics}}
	if w := createAutomationToken(t, metricsOnly, s, `["report"]`); w.Code != http.StatusBadRequest {
		t.Fatalf("scope this app lacks: %d", w.Code)
	}
	if w := createAutomationToken(t, metricsOnly, s, `["unknown"]`); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown scope: %d", w.Code)
	}
	unsignedHandler := &Handler{Store: s}
	if w := createAutomationToken(t, unsignedHandler, s, `["read"]`); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("no signing key: %d", w.Code)
	}
	failing := &Handler{Store: s, SigningKey: func() (ed25519.PrivateKey, error) { return nil, errors.New("private key path") }}
	if w := createAutomationToken(t, failing, s, `["read"]`); w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("key failure: %d %s", w.Code, w.Body.String())
	}
}

func TestLoadOrCreateSigningKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "automation-token.key")
	created, err := LoadOrCreateSigningKey(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadOrCreateSigningKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if !created.Equal(loaded) {
		t.Fatal("key changed between loads")
	}
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateSigningKey(path); err == nil {
		t.Fatal("accepted a corrupt key file")
	}
}
