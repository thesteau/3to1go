// Package static embeds the web UI static assets.
package static

import "embed"

//go:embed index.html css html js
var Files embed.FS
