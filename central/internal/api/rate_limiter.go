package api

import (
	"strings"

	"github.com/3to1go/shared/httpx"
)

func specsForPath(path string) []httpx.RateSpec {
	var specs []httpx.RateSpec
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/backup/") {
		specs = append(specs, httpx.RateSpec{Name: "all-api", PerSecond: 50, Burst: 600, RetryAfter: 1})
	}
	switch path {
	case "/api/session/login":
		specs = append(specs, httpx.RateSpec{Name: "login", PerSecond: 5.0 / 60.0, Burst: 5, RetryAfter: 60})
	case "/backup/uploads/initiate":
		specs = append(specs, httpx.RateSpec{Name: "upload-initiate", PerSecond: 1, Burst: 20, RetryAfter: 10})
	}
	return specs
}
