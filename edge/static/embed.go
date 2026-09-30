// Package static embeds the web UI static assets.
package static

import "embed"

// js/ is compiled from ts/ and is not committed; run `npm ci` once, then `go generate ./...`.
//go:generate npx tsc -p .

//go:embed index.html css html js img
var Files embed.FS

// staticFiles returns the embedded filesystem (used by api package).
func staticFiles() embed.FS { return Files }
