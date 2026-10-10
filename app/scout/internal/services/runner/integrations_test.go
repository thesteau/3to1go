package runner

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/3to1go/scout/internal/backup"
	"github.com/3to1go/scout/internal/services/state"
	"github.com/3to1go/shared/integrations"
)

func TestPreAndPostDestinationsRunIndependently(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("INTEGRATIONS_KEY_FILE", "")
	seen := make(chan struct {
		path  string
		event integrations.Event
	}, 4)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event integrations.Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Error(err)
		}
		seen <- struct {
			path  string
			event integrations.Event
		}{r.URL.Path, event}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	settings := testRunnerSettings(t)
	runner := testRunner(t, settings, nil)
	manager, err := integrations.New("scout", t.TempDir(), runner.logger, func() *tls.Config {
		return server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	runner.Integrations = manager
	for _, action := range []struct{ path, event string }{{"/prepare", integrations.JobStarted}, {"/report", integrations.JobFinished}} {
		endpoint := server.URL + action.path
		if _, err := manager.Save(integrations.Update{Destination: integrations.Destination{Name: action.path, Enabled: true, Format: "json", Events: []string{action.event}}, URL: &endpoint}); err != nil {
			t.Fatal(err)
		}
	}
	job := &backup.JobDefinition{RootPath: filepath.Join(settings.ScanRoot, "job"), JobName: "job"}
	if err := os.MkdirAll(job.RootPath, 0o755); err != nil {
		t.Fatal(err)
	}
	runner.prepareJob(job, settings, make(chan *uploadWork, 1))
	for _, want := range []struct{ path, event, status string }{{"/prepare", integrations.JobStarted, "started"}, {"/report", integrations.JobFinished, "skipped_empty"}} {
		select {
		case got := <-seen:
			if got.path != want.path || got.event.Type != want.event || got.event.Status != want.status || got.event.JobName != job.JobName {
				t.Fatalf("wrong independent action: %+v", got)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("missing action")
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	done := runner.beginOperation(cancelled)
	runner.publishJobStarted(job, settings)
	done()
	manager.Close()
	if len(seen) != 0 {
		t.Fatal("cancelled attempt emitted PRE")
	}
}

func TestFinishedJobsSendSuccessAndFailureWithoutSettingsSecrets(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("INTEGRATIONS_KEY_FILE", "")
	seen := make(chan []byte, 4)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Error(err)
		}
		seen <- event
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	settings := testRunnerSettings(t)
	settings.ScoutCredential = "private-token"
	settings.AdvertisedURL = "https://private-url.invalid"
	runner := testRunner(t, settings, nil)
	manager, err := integrations.New("scout", t.TempDir(), runner.logger, func() *tls.Config {
		return server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	runner.Integrations = manager
	if _, err := manager.Save(integrations.Update{Destination: integrations.Destination{Name: "Events", Enabled: true, Format: "json", Events: []string{integrations.JobFinished, integrations.UploadFinished}}, URL: &server.URL}); err != nil {
		t.Fatal(err)
	}
	job := &backup.JobDefinition{RootPath: filepath.Join(settings.ScanRoot, "job"), JobName: "job"}
	for _, status := range []string{"success", "retry_scheduled"} {
		runner.saveState(job, state.JobState{LastStatus: status, LastStoredAs: "stored.tar.zst", LastErrorDetail: "private-detail"})
		runner.finishJob(job, settings)
	}
	want := []struct{ event, status, storedAs string }{{integrations.JobFinished, "success", "stored.tar.zst"}, {integrations.UploadFinished, "success", "stored.tar.zst"}, {integrations.JobFinished, "retry_scheduled", ""}}
	for _, expected := range want {
		select {
		case payload := <-seen:
			var event integrations.Event
			if err := json.Unmarshal(payload, &event); err != nil {
				t.Fatal(err)
			}
			if event.Type != expected.event || event.Status != expected.status || event.StoredAs != expected.storedAs || event.ScoutID != settings.ScoutID || event.ScoutInstanceID == "" || event.JobName != job.JobName {
				t.Fatalf("wrong job event: %+v", event)
			}
			if strings.Contains(string(payload), "private") {
				t.Fatal("job event contains private settings or details")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("missing job notification")
		}
	}
}
