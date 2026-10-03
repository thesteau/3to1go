package api

import (
	"github.com/3to1go/edge/static"
	"github.com/3to1go/shared/webui"
)

var readStaticFile = webui.Reader(static.Files, "css/variables.css", "css/base.css", "css/jobs.css", "css/directories.css", "css/dialogs.css", "css/toasts.css", "css/users.css", "css/recovery.css", "css/responsive.css")

// Serve the page shell and static files with cache tags, so a refresh only
// downloads what changed.
var (
	serveIndex  = webui.IndexServer(readStaticFile)
	staticFiles = webui.StaticHandler(static.Files)
)
