// Package transfer observes streaming byte counts without buffering the stream.
package transfer

import (
	"context"
	"io"
	"time"

	"github.com/3to1go/scout/internal/cancelio"
)

type reporterKey struct{}

func WithReporter(ctx context.Context, report func(done, total int64)) context.Context {
	return context.WithValue(ctx, reporterKey{}, report)
}

// Observe reports at most once per percentage point or every 200ms. An unknown
// total is reported as zero, so callers can show indeterminate progress.
func Observe(ctx context.Context, input io.Reader, total int64) *reader {
	source := cancelio.Reader{Context: ctx, Reader: input}
	report, _ := ctx.Value(reporterKey{}).(func(int64, int64))
	total = max(0, total)
	if report != nil {
		report(0, total)
	}
	return &reader{source: source, report: report, total: total, lastAt: time.Now()}
}

type reader struct {
	source      io.Reader
	report      func(int64, int64)
	total, done int64
	lastPercent int
	lastAt      time.Time
}

func (r *reader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	r.done += int64(n)
	if r.report == nil {
		return n, err
	}
	percent := 0
	if r.total > 0 {
		percent = int(min(100, float64(r.done)/float64(r.total)*100))
	}
	if percent != r.lastPercent || time.Since(r.lastAt) >= 200*time.Millisecond || err != nil {
		r.report(r.done, r.total)
		r.lastPercent, r.lastAt = percent, time.Now()
	}
	return n, err
}

// Flush publishes the last byte count when a format parser finishes without
// reading through the underlying stream's EOF.
func (r *reader) Flush() {
	if r.report != nil {
		r.report(r.done, r.total)
	}
}
