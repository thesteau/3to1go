// Package metrics serves opt-in Prometheus metrics without using the global registry.
package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/3to1go/shared/buildinfo"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Gauge is a value read from current application state on each scrape.
type Gauge struct {
	Name   string
	Help   string
	Value  float64
	Labels map[string]string
}

func NewGauge(name, help string, value float64, labels map[string]string) Gauge {
	return Gauge{Name: name, Help: help, Value: value, Labels: labels}
}

type snapshot []Gauge

func (s snapshot) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(s, ch)
}

func (s snapshot) Collect(ch chan<- prometheus.Metric) {
	for _, gauge := range s {
		desc := prometheus.NewDesc(gauge.Name, gauge.Help, nil, prometheus.Labels(gauge.Labels))
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, gauge.Value)
	}
}

// NewHandler checks the current setting before collecting anything. Snapshots
// have their own registry per request so removed jobs leave no stale series.
func NewHandler(app string, enabled func() bool, collect func(context.Context) ([]Gauge, error)) http.Handler {
	runtime := prometheus.NewRegistry()
	runtime.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	runtime.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "three_to_one_go_build_info",
		Help: "Application build information.",
		ConstLabels: prometheus.Labels{
			"app": app, "version": buildinfo.Version, "commit": buildinfo.Commit,
		},
	}, func() float64 { return 1 }))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !enabled() {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		values, err := collect(r.Context())
		if err != nil {
			// Keep storage errors and configuration details out of this public endpoint.
			http.Error(w, "metrics collection failed", http.StatusServiceUnavailable)
			return
		}
		registry := prometheus.NewRegistry()
		registry.MustRegister(snapshot(values))
		promhttp.HandlerFor(prometheus.Gatherers{runtime, registry}, promhttp.HandlerOpts{}).ServeHTTP(w, r)
	})
}

func Bool(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

// Timestamp returns Unix seconds, or zero when an event has not happened yet.
func Timestamp(value string) float64 {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}
	return float64(t.Unix())
}
