// Package static embeds the web UI static assets.
package static

import "embed"

// js/ is compiled from ts/ and is not committed; run `npm ci` once, then `go generate ./...`.
//go:generate npx tsc -p .
//go:generate node copy-libs.cts

//go:embed index.html css html js img
var Files embed.FS
