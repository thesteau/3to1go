package runner

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/3to1go/scout/internal/backup"
	"github.com/3to1go/scout/internal/services/state"
	"github.com/3to1go/shared/integrations"
)

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
