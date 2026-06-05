// Package static embeds the web UI static assets.
package static

import "embed"

//go:embed index.html app.js app.css
var Files embed.FS
