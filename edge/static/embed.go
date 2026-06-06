// Package static embeds the web UI static assets.
package static

import "embed"

//go:embed index.html css html js img
var Files embed.FS

// staticFiles returns the embedded filesystem (used by api package).
func staticFiles() embed.FS { return Files }
