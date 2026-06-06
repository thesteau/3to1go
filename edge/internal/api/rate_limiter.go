package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type rateSpec struct {
	name       string
	perSecond  float64
	burst      float64
	retryAfter int
}

type rateBucket struct {
	tokens float64
	last   time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]rateBucket
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: map[string]rateBucket{}}
}

func (l *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client := clientIP(r)
		for _, spec := range specsForPath(r.URL.Path) {
			if ok, retryAfter := l.allow(client+"|"+spec.name, spec); !ok {
				w.Header().Set("Retry-After", retryAfterHeader(retryAfter))
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (l *rateLimiter) allow(key string, spec rateSpec) (bool, int) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.buckets[key]
	if b.last.IsZero() {
		b = rateBucket{tokens: spec.burst, last: now}
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = min(spec.burst, b.tokens+elapsed*spec.perSecond)
	b.last = now

	if b.tokens < 1 {
		l.buckets[key] = b
		return false, spec.retryAfter
	}
	b.tokens--
	l.buckets[key] = b
	return true, 0
}

func specsForPath(path string) []rateSpec {
	var specs []rateSpec
	if strings.HasPrefix(path, "/api/") {
		specs = append(specs, rateSpec{name: "all-api", perSecond: 10, burst: 120, retryAfter: 1})
	}
	if path == "/api/session/login" {
		specs = append(specs, rateSpec{name: "login", perSecond: 5.0 / 60.0, burst: 5, retryAfter: 60})
	}
	return specs
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "unknown"
}

func retryAfterHeader(seconds int) string {
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}
