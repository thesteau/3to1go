package api

import (
	"github.com/3to1go/shared/webui"
	"github.com/3to1go/station/static"
)

var readStaticFile = webui.Reader(static.Files, "css/variables.css", "css/base.css", "css/dialogs.css", "css/toasts.css", "css/users.css", "css/scouts.css", "css/snapshots.css", "css/responsive.css")

// Serve the page shell and static files with cache tags, so a refresh only
// downloads what changed.
var (
	serveIndex  = webui.IndexServer(readStaticFile)
	staticFiles = webui.StaticHandler(static.Files)
)
