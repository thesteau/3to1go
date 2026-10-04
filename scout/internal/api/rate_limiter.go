package api

import (
	"strings"

	"github.com/3to1go/shared/httpx"
)

func specsForPath(path string) []httpx.RateSpec {
	var specs []httpx.RateSpec
	if strings.HasPrefix(path, "/api/") {
		specs = append(specs, httpx.RateSpec{Name: "all-api", PerSecond: 50, Burst: 600, RetryAfter: 1})
	}
	switch path {
	case "/api/session/login":
		specs = append(specs, httpx.RateSpec{Name: "login", PerSecond: 5.0 / 60.0, Burst: 5, RetryAfter: 60})
	case "/backup/restore-notifications":
		// Public, and each call asks Station for the request list.
		specs = append(specs, httpx.RateSpec{Name: "restore-notification", PerSecond: 1, Burst: 10, RetryAfter: 5})
	}
	return specs
}
