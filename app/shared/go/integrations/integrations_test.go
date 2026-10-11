package integrations

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/3to1go/shared/auth"
	"github.com/go-chi/chi/v5"
)

func testManager(t *testing.T, configDir string, server *httptest.Server, logs io.Writer) *Manager {
	t.Helper()
	var trust func() *tls.Config
	if server != nil {
		trust = func() *tls.Config { return server.Client().Transport.(*http.Transport).TLSClientConfig.Clone() }
	}
	m, err := New("scout", configDir, slog.New(slog.NewTextHandler(logs, nil)), trust)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

func saveDestination(t *testing.T, m *Manager, endpoint, format string) Destination {
	t.Helper()
	d, err := m.Save(Update{Destination: Destination{Name: "Example", Enabled: true, Format: format, Events: []string{UploadFinished}}, URL: &endpoint})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestEncryptedPersistenceAndWriteOnlyAPI(t *testing.T) {
	t.Setenv("INTEGRATIONS_KEY_FILE", "")
	dir := t.TempDir()
	m := testManager(t, dir, nil, io.Discard)
	endpoint := "https://example.invalid/secret-path?token=url-secret"
	headers := map[string]string{"Authorization": "Bearer header-secret"}
	router := chi.NewRouter()
	Register(router, m, "scout")
	payload, _ := json.Marshal(Update{Destination: Destination{Name: "Example", Enabled: true, Format: "json", Events: []string{UploadFinished}}, URL: &endpoint, Headers: &headers})
	create := httptest.NewRequest(http.MethodPost, "/api/integrations", bytes.NewReader(payload))
	create = create.WithContext(context.WithValue(create.Context(), auth.ContextKeyUser, &auth.User{ID: 1, IsAdmin: true}))
	saved := httptest.NewRecorder()
	router.ServeHTTP(saved, create)
	if saved.Code != http.StatusOK {
		t.Fatalf("create status %d: %s", saved.Code, saved.Body.String())
	}
	var result struct {
		Destination Destination `json:"destination"`
	}
	if err := json.Unmarshal(saved.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	d := result.Destination
	request := httptest.NewRequest(http.MethodGet, "/api/integrations", nil)
	request = request.WithContext(context.WithValue(request.Context(), auth.ContextKeyUser, &auth.User{ID: 1, IsAdmin: true}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response status %d", response.Code)
	}
	onDisk, err := os.ReadFile(filepath.Join(dir, "integrations", "destinations.enc"))
	if err != nil {
		t.Fatal(err)
	}
	safeJSON, _ := json.Marshal(d)
	for _, secret := range []string{"url-secret", "header-secret", "secret-path", "Authorization"} {
		if bytes.Contains(onDisk, []byte(secret)) || bytes.Contains(safeJSON, []byte(secret)) || strings.Contains(response.Body.String(), secret) || strings.Contains(saved.Body.String(), secret) {
			t.Fatalf("secret exposed: %s", secret)
		}
	}
	// Editing metadata with omitted secret fields keeps credentials.
	d.Name = "Renamed"
	if _, err := m.Save(Update{Destination: d}); err != nil {
		t.Fatal(err)
	}
	reopened := testManager(t, dir, nil, io.Discard)
	if reopened.destinations[0].URL != endpoint || reopened.destinations[0].Headers["Authorization"] != headers["Authorization"] {
		t.Fatal("secrets were lost during edit/reload")
	}
	empty := map[string]string{}
	if _, err := reopened.Save(Update{Destination: d, Headers: &empty}); err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot()[0].HeadersConfigured {
		t.Fatal("headers were not cleared")
	}
	if err := reopened.Delete(d.ID); err != nil {
		t.Fatal(err)
	}
	if len(testManager(t, dir, nil, io.Discard).Snapshot()) != 0 {
		t.Fatal("deleted integration returned after restart")
	}
}

func TestMissingOrWrongKeyFailsClosed(t *testing.T) {
	t.Setenv("INTEGRATIONS_KEY_FILE", "")
	dir := t.TempDir()
	m := testManager(t, dir, nil, io.Discard)
	saveDestination(t, m, "https://example.invalid/secret", "json")
	keyPath := filepath.Join(dir, "integrations", "key")
	key, _ := os.ReadFile(keyPath)
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	if _, err := New("scout", dir, slog.Default(), nil); err == nil {
		t.Fatal("missing key accepted")
	}
	if _, err := m.Save(Update{Destination: m.Snapshot()[0]}); err == nil {
		t.Fatal("running manager recreated a missing key")
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatal("a replacement key was silently generated")
	}
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("00", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New("scout", dir, slog.Default(), nil); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err := m.Save(Update{Destination: m.Snapshot()[0]}); err == nil {
		t.Fatal("running manager accepted a changed key")
	}
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New("station", dir, slog.Default(), nil); err == nil {
		t.Fatal("ciphertext accepted for another app")
	}
	reloaded := testManager(t, dir, nil, io.Discard)
	if len(reloaded.Snapshot()) != 1 {
		t.Fatal("original key did not recover integrations")
	}
}

func TestExternalKeyNeverGenerated(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "mounted-key")
	t.Setenv("INTEGRATIONS_KEY_FILE", keyPath)
	m := testManager(t, dir, nil, io.Discard)
	endpoint := "https://example.invalid/token"
	update := Update{Destination: Destination{Name: "Test", Format: "json", Events: []string{UploadFinished}}, URL: &endpoint}
	if _, err := m.Save(update); err == nil {
		t.Fatal("missing external key accepted")
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatal("external key was created")
	}
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("ab", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Save(update); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "integrations", "key")); !os.IsNotExist(err) {
		t.Fatal("local key created with external key configured")
	}
}

func TestTamperedCiphertextFailsClosedAndFilesArePrivate(t *testing.T) {
	t.Setenv("INTEGRATIONS_KEY_FILE", "")
	dir := t.TempDir()
	m := testManager(t, dir, nil, io.Discard)
	saveDestination(t, m, "https://example.invalid/private-token", "json")
	if runtime.GOOS != "windows" {
		for _, name := range []string{"key", "destinations.enc"} {
			info, err := os.Stat(filepath.Join(dir, "integrations", name))
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("integration file must be owner-only: %s", name)
			}
		}
	}
	path := filepath.Join(dir, "integrations", "destinations.enc")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New("scout", dir, slog.Default(), nil); err != errVault {
		t.Fatalf("tampered ciphertext must fail with a safe error: %v", err)
	}
}

func TestSlowReceiverTimesOutWithoutExposingURL(t *testing.T) {
	t.Setenv("INTEGRATIONS_KEY_FILE", "")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(server.Close)
	m := testManager(t, t.TempDir(), server, io.Discard)
	d := saveDestination(t, m, server.URL+"/private-token", "json")
	d.TimeoutSeconds = 1
	if _, err := m.Save(Update{Destination: d}); err != nil {
		t.Fatal(err)
	}
	if err := m.Test(context.Background(), d.ID); err == nil || err.Error() != "notification timed out" {
		t.Fatalf("expected a safe timeout error: %v", err)
	}
}

func TestFormatsEncodeDataAndOmitPrivateDetails(t *testing.T) {
	for _, format := range []string{"json", "text", "custom-json"} {
		t.Run(format, func(t *testing.T) {
			t.Setenv("INTEGRATIONS_KEY_FILE", "")
			requests := make(chan *http.Request, 1)
			bodies := make(chan []byte, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				requests <- r
				bodies <- body
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(server.Close)
			m := testManager(t, t.TempDir(), server, io.Discard)
			d := saveDestination(t, m, server.URL, format)
			event := Event{Type: UploadFinished, App: "scout", JobName: "quotes\"\n@everyone {{ detail }}", Status: "success", Detail: "PRIVATE_DETAIL", SourceAddress: "PRIVATE_IP"}
			if err := m.deliver(context.Background(), m.destinations[0], event); err != nil {
				t.Fatal(err)
			}
			request, body := <-requests, <-bodies
			if request.Method != http.MethodPost || bytes.Contains(body, []byte("PRIVATE_DETAIL")) || bytes.Contains(body, []byte("PRIVATE_IP")) {
				t.Fatalf("unsafe payload: %s", body)
			}
			if format != "text" {
				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatal(err)
				}
				if format == "custom-json" && payload["message"] != render(DefaultMessageTemplate, Event{Type: event.Type, App: event.App, JobName: event.JobName, Status: event.Status}) {
					t.Fatal("generic default message missing")
				}
				if format == "json" && payload["job_name"] != event.JobName {
					t.Fatal("job name was not preserved")
				}
			}
			d.IncludeDetail = true
			if _, err := m.Save(Update{Destination: d}); err != nil {
				t.Fatal(err)
			}
			event.JobName = "Job"
			if err := m.deliver(context.Background(), m.destinations[0], event); err != nil {
				t.Fatal(err)
			}
			<-requests
			body = <-bodies
			if format == "json" && !bytes.Contains(body, []byte("PRIVATE_DETAIL")) {
				t.Fatal("opt-in details missing")
			}
		})
	}
}

func TestHTTPFailuresDoNotReturnSecretsOrFollowRedirects(t *testing.T) {
	t.Setenv("INTEGRATIONS_KEY_FILE", "")
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusTemporaryRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", "https://example.invalid/leaked-secret")
				w.WriteHeader(status)
				_, _ = w.Write([]byte("echoed-secret"))
			}))
			t.Cleanup(server.Close)
			m := testManager(t, t.TempDir(), server, io.Discard)
			d := saveDestination(t, m, server.URL+"/url-secret", "json")
			err := m.Test(context.Background(), d.ID)
			if err == nil || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "HTTP") {
				t.Fatalf("unsafe or absent failure: %v", err)
			}
		})
	}
	m := testManager(t, t.TempDir(), nil, io.Discard)
	d := saveDestination(t, m, "https://127.0.0.1:1/url-secret", "json")
	if err := m.Test(context.Background(), d.ID); err == nil || strings.Contains(err.Error(), "url-secret") {
		t.Fatalf("unsafe connection error: %v", err)
	}
}

func TestAsyncDeliveryFiltersAndSafeLogs(t *testing.T) {
	t.Setenv("INTEGRATIONS_KEY_FILE", "")
	seen := make(chan Event, 4)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event Event
		_ = json.NewDecoder(r.Body).Decode(&event)
		seen <- event
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("secret-response"))
	}))
	t.Cleanup(server.Close)
	var logs bytes.Buffer
	m := testManager(t, t.TempDir(), server, &logs)
	d := saveDestination(t, m, server.URL+"/secret-url", "json")
	d.MatchJobName, d.MatchScoutID, d.MatchInstanceID, d.MatchSourceAddress = "wanted", "scout", "instance", "source"
	if _, err := m.Save(Update{Destination: d}); err != nil {
		t.Fatal(err)
	}
	m.Publish(Event{Type: UploadFinished, JobName: "other"})
	m.Publish(Event{Type: JobFinished, JobName: "wanted"})
	start := time.Now()
	m.Publish(Event{Type: UploadFinished, JobName: "wanted", ScoutID: "scout", ScoutInstanceID: "instance", SourceAddress: "source"})
	if time.Since(start) > time.Second {
		t.Fatal("Publish blocked on HTTP")
	}
	select {
	case event := <-seen:
		if event.ID == "" || event.Time == "" || event.App != "scout" {
			t.Fatal("event metadata missing")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("notification not delivered")
	}
	m.Close()
	if strings.Contains(logs.String(), "secret") || !strings.Contains(logs.String(), "integration_delivery_failed") {
		t.Fatalf("unsafe/missing logs: %s", logs.String())
	}
	if len(seen) != 0 {
		t.Fatal("non-matching events were delivered")
	}
}

func TestIntegrationRoutesRequireAdmin(t *testing.T) {
	for _, user := range []*auth.User{nil, {ID: 2}, {ID: 1, IsAdmin: true, MustChangePassword: true}} {
		for _, route := range []struct{ method, path string }{{"GET", "/api/integrations"}, {"POST", "/api/integrations"}, {"DELETE", "/api/integrations/id"}, {"POST", "/api/integrations/id/test"}} {
			router := chi.NewRouter()
			Register(router, nil, "scout")
			request := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
			if user != nil {
				request = request.WithContext(context.WithValue(request.Context(), auth.ContextKeyUser, user))
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			want := http.StatusForbidden
			if user == nil {
				want = http.StatusUnauthorized
			}
			if response.Code != want {
				t.Fatalf("%s %s: status %d, want %d", route.method, route.path, response.Code, want)
			}
		}
	}
}

func TestValidationRejectsInsecureDestinationsAndHeaders(t *testing.T) {
	t.Setenv("INTEGRATIONS_KEY_FILE", "")
	m := testManager(t, t.TempDir(), nil, io.Discard)
	for _, endpoint := range []string{"http://example.invalid/token", "file:///etc/passwd", "https://user:password@example.invalid", "https://example.invalid/#secret", "https://"} {
		if _, err := m.Save(Update{Destination: Destination{Name: "Test", Format: "json", Events: []string{UploadFinished}}, URL: &endpoint}); err == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
	d := saveDestination(t, m, "https://example.invalid", "json")
	for _, headers := range []map[string]string{{"Authorization": "token\r\nInjected: value"}, {"Host": "elsewhere"}, {"Content-Type": "wrong"}, {"bad name": "value"}, {"X-Key": "one", "x-key": "two"}} {
		if _, err := m.Save(Update{Destination: d, Headers: &headers}); err == nil {
			t.Fatal("unsafe header accepted")
		}
	}
	d.Events = []string{UploadReceived}
	if _, err := m.Save(Update{Destination: d}); err == nil {
		t.Fatal("Station event accepted by Scout")
	}
}

func TestCustomJSONRendersStringValuesWithoutChangingStructure(t *testing.T) {
	fields := templateFields(Event{JobName: "quotes\"\n{{ detail }}", Detail: "PRIVATE", Status: "success"})
	fields["message"] = "Job: " + fields["job_name"]
	payload, err := renderJSON(`{"text":"{{ message }}","nested":[{"job":"{{ job_name }}","unknown":"{{ unknown }}"}],"count":9007199254740993,"enabled":true,"empty":null,"{{ job_name }}":"literal key"}`, fields)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Text    string              `json:"text"`
		Nested  []map[string]string `json:"nested"`
		Count   json.Number         `json:"count"`
		Enabled bool                `json:"enabled"`
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.Text != fields["message"] || got.Nested[0]["job"] != fields["job_name"] || got.Nested[0]["unknown"] != "" || got.Count.String() != "9007199254740993" || !got.Enabled {
		t.Fatalf("template changed data: %s", payload)
	}
	if !bytes.Contains(payload, []byte(`"{{ job_name }}":"literal key"`)) || bytes.Contains(payload, []byte("PRIVATE")) {
		t.Fatalf("keys or substituted tokens were interpreted: %s", payload)
	}
	for _, template := range []string{`{"job":{{ job_name }}}`, `{"text":"ok"} {}`, `not JSON`} {
		if _, err := renderJSON(template, fields); err == nil {
			t.Fatal("invalid JSON accepted")
		}
	}
}

func TestCustomJSONDeliveryUsesOwnTemplateAndDetailOptIn(t *testing.T) {
	t.Setenv("INTEGRATIONS_KEY_FILE", "")
	seen := make(chan map[string]string, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("wrong custom JSON content type")
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		seen <- payload
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	m := testManager(t, t.TempDir(), server, io.Discard)
	d := saveDestination(t, m, server.URL, "custom-json")
	d.MessageTemplate = "{{ job_name }}: {{ status }}"
	d.PayloadTemplate = `{"text":"{{ message }}","detail":"{{ detail }}"}`
	event := Event{JobName: "quotes\"\n{{ detail }}", Status: "success", Detail: "PRIVATE"}
	for _, detail := range []bool{false, true} {
		d.IncludeDetail = detail
		if _, err := m.Save(Update{Destination: d}); err != nil {
			t.Fatal(err)
		}
		if err := m.deliver(context.Background(), m.destinations[0], event); err != nil {
			t.Fatal(err)
		}
		got := <-seen
		if got["text"] != event.JobName+": success" || (got["detail"] == "PRIVATE") != detail {
			t.Fatalf("wrong custom body: %+v", got)
		}
	}
}

func TestDefaultsAndIndependentPrePostActions(t *testing.T) {
	t.Setenv("INTEGRATIONS_KEY_FILE", "")
	m := testManager(t, t.TempDir(), nil, io.Discard)
	post := saveDestination(t, m, "https://example.invalid/after", "custom-json")
	if post.MessageTemplate != DefaultMessageTemplate || post.PayloadTemplate != DefaultPayloadTemplate {
		t.Fatal("new destination has no generic defaults")
	}
	endpoint := "https://example.invalid/before"
	pre, err := m.Save(Update{Destination: Destination{Name: "Before", Enabled: true, Format: "text", Events: []string{JobStarted}, MessageTemplate: "Preparing {{ job_name }}"}, URL: &endpoint})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Snapshot()) != 2 || pre.MessageTemplate == post.MessageTemplate {
		t.Fatal("independent actions were combined")
	}
	pre.Events = append(pre.Events, JobFinished)
	if _, err := m.Save(Update{Destination: pre}); err == nil {
		t.Fatal("mixed PRE and POST action accepted")
	}
	post.PayloadTemplate = `{"text": {{ message }}}`
	if _, err := m.Save(Update{Destination: post}); err == nil {
		t.Fatal("invalid body template accepted")
	}
	if m.Snapshot()[1].MessageTemplate != "Preparing {{ job_name }}" {
		t.Fatal("another action was overwritten")
	}
}

func TestLegacyReceiverLoadsAsGenericJSON(t *testing.T) {
	t.Setenv("INTEGRATIONS_KEY_FILE", "")
	dir := t.TempDir()
	m := testManager(t, dir, nil, io.Discard)
	d := saveDestination(t, m, "https://example.invalid/private-token", "json")
	m.destinations[0].Format = "discord"
	m.destinations[0].MessageTemplate = "Existing {{ job_name }} message"
	if err := m.persist(m.destinations); err != nil {
		t.Fatal(err)
	}
	reopened := testManager(t, dir, nil, io.Discard)
	got := reopened.Snapshot()[0]
	if got.ID != d.ID || got.Format != "custom-json" || got.MessageTemplate != "Existing {{ job_name }} message" || !got.URLConfigured {
		t.Fatal("legacy destination was lost")
	}
	fields := templateFields(Event{JobName: "Job"})
	fields["message"] = render(got.MessageTemplate, Event{JobName: "Job"})
	payload, err := renderJSON(got.PayloadTemplate, fields)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Content         string `json:"content"`
		AllowedMentions struct {
			Parse []string `json:"parse"`
		} `json:"allowed_mentions"`
	}
	if err := json.Unmarshal(payload, &body); err != nil || body.Content != "Existing Job message" || body.AllowedMentions.Parse == nil || len(body.AllowedMentions.Parse) != 0 {
		t.Fatalf("legacy body changed: %s", payload)
	}
	if _, err := reopened.Save(Update{Destination: got}); err != nil {
		t.Fatal(err)
	}
	if testManager(t, dir, nil, io.Discard).Snapshot()[0].Format != "custom-json" {
		t.Fatal("generic destination was not persisted")
	}
}
