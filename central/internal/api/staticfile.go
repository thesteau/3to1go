package api

import (
	"github.com/3to1go/central/static"
	"github.com/3to1go/shared/webui"
	"sync"
)

var pageShell = sync.OnceValues(func() ([]byte, error) {
	return webui.Index(static.Files,
		"css/variables.css",
		"css/base.css",
		"css/dialogs.css",
		"css/toasts.css",
		"css/users.css",
		"css/edges.css",
		"css/snapshots.css",
		"css/responsive.css",
	)
})

func readStaticFile(name string) ([]byte, error) {
	if name == "index.html" {
		return pageShell()
	}
	return static.Files.ReadFile(name)
}
