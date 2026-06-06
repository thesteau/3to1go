// Package static embeds the web UI static assets.
package static

import "embed"

//go:embed index.html css html js img
var Files embed.FS
