package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func TestHandlerOptInAndFreshSnapshots(t *testing.T) {
	enabled := false
	label := "folder/with \"quotes\"\\and\nnewlines"
	values := []Gauge{NewGauge("example_jobs", "Jobs.", 2, map[string]string{"job": label})}
	calls := 0
	handler := NewHandler("scout", func() bool { return enabled }, func(context.Context) ([]Gauge, error) {
		calls++
		return values, nil
	})
	scrape := func() *httptest.ResponseRecorder {
		t.Helper()
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if rr.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("metrics responses must not be cached")
		}
		return rr
	}
	if rr := scrape(); rr.Code != http.StatusNotFound || calls != 0 {
		t.Fatalf("disabled: status %d, collections %d", rr.Code, calls)
	}
	enabled = true
	rr := scrape()
	if rr.Code != http.StatusOK || !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("enabled: %d %s", rr.Code, rr.Body.String())
	}
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(rr.Body)
	if err != nil {
		t.Fatal(err)
	}
	metric := families["example_jobs"].GetMetric()[0]
	if metric.GetGauge().GetValue() != 2 || metric.GetLabel()[0].GetValue() != label {
		t.Fatalf("gauge or label did not round-trip: %v", metric)
	}
	for _, name := range []string{"go_goroutines", "process_start_time_seconds", "three_to_one_go_build_info"} {
		if families[name] == nil {
			t.Errorf("missing %s", name)
		}
	}
	values = nil
	if rr := scrape(); rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "example_jobs") {
		t.Fatal("removed jobs must disappear from subsequent scrapes")
	}
	enabled = false
	if rr := scrape(); rr.Code != http.StatusNotFound || calls != 2 {
		t.Fatalf("disabled again: status %d, collections %d", rr.Code, calls)
	}
}

func TestHandlerErrorsAndMethods(t *testing.T) {
	calls := 0
	handler := NewHandler("station", func() bool { return true }, func(context.Context) ([]Gauge, error) {
		calls++
		return nil, errors.New("private database connection details")
	})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if rr.Code != http.StatusMethodNotAllowed || rr.Header().Get("Allow") != "GET" || calls != 0 {
		t.Fatalf("POST: status %d, collections %d", rr.Code, calls)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), "private") {
		t.Fatalf("failed scrape must return a generic 503: %d %s", rr.Code, rr.Body.String())
	}
}

func TestConcurrentScrapes(t *testing.T) {
	handler := NewHandler("scout", func() bool { return true }, func(context.Context) ([]Gauge, error) {
		return []Gauge{NewGauge("example_jobs", "Jobs.", 2, nil)}, nil
	})
	for i := range 12 {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "example_jobs 2") {
				t.Fatalf("concurrent scrape: %d %s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestTimestamp(t *testing.T) {
	for _, value := range []string{"2026-10-15T00:00:00Z", "2026-10-15T01:00:00+01:00", "2026-10-15T00:00:00.123Z"} {
		if got := Timestamp(value); got != 1792022400 {
			t.Errorf("Timestamp(%q) = %v", value, got)
		}
	}
	for _, value := range []string{"", "not a timestamp"} {
		if got := Timestamp(value); got != 0 {
			t.Errorf("Timestamp(%q) = %v, want zero", value, got)
		}
	}
}
