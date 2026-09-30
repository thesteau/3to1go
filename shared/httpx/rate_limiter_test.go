package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimiterPolicyAndClientIsolation(t *testing.T) {
	limiter := NewRateLimiter(func(path string) []RateSpec {
		if path != "/limited" {
			return nil
		}
		return []RateSpec{{Name: "login", Burst: 1, RetryAfter: 60}}
	})
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		path, remote string
		want         int
	}{
		{"/limited", "192.0.2.1:1000", 204},
		{"/limited", "192.0.2.1:2000", 429},
		{"/limited", "192.0.2.2:1000", 204},
		{"/public", "192.0.2.1:1000", 204},
	} {
		request := httptest.NewRequest("GET", tc.path, nil)
		request.RemoteAddr = tc.remote
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.want {
			t.Errorf("%s %s: got %d, want %d", tc.remote, tc.path, response.Code, tc.want)
		}
		if tc.want == 429 && response.Header().Get("Retry-After") != "60" {
			t.Error("missing retry interval")
		}
	}
}
