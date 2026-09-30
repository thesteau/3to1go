package api

import (
	"strings"

	"github.com/3to1go/shared/httpx"
)

func specsForPath(path string) []httpx.RateSpec {
	var specs []httpx.RateSpec
	if strings.HasPrefix(path, "/api/") {
		specs = append(specs, httpx.RateSpec{Name: "all-api", PerSecond: 10, Burst: 120, RetryAfter: 1})
	}
	if path == "/api/session/login" {
		specs = append(specs, httpx.RateSpec{Name: "login", PerSecond: 5.0 / 60.0, Burst: 5, RetryAfter: 60})
	}
	return specs
}
