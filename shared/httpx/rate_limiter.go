package httpx

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type RateSpec struct {
	Name       string
	PerSecond  float64
	Burst      float64
	RetryAfter int
}

type rateBucket struct {
	tokens float64
	last   time.Time
}

type RateLimiter struct {
	mu           sync.Mutex
	buckets      map[string]rateBucket
	specsForPath func(string) []RateSpec
}

func NewRateLimiter(specs func(string) []RateSpec) *RateLimiter {
	return &RateLimiter{buckets: map[string]rateBucket{}, specsForPath: specs}
}

func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client := clientIP(r)
		for _, spec := range l.specsForPath(r.URL.Path) {
			if ok, retryAfter := l.allow(client+"|"+spec.Name, spec); !ok {
				w.Header().Set("Retry-After", retryAfterHeader(retryAfter))
				WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (l *RateLimiter) allow(key string, spec RateSpec) (bool, int) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.buckets[key]
	if b.last.IsZero() {
		b = rateBucket{tokens: spec.Burst, last: now}
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = min(spec.Burst, b.tokens+elapsed*spec.PerSecond)
	b.last = now

	if b.tokens < 1 {
		l.buckets[key] = b
		return false, spec.RetryAfter
	}
	b.tokens--
	l.buckets[key] = b
	return true, 0
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
