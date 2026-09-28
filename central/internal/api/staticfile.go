package api

import (
	"github.com/3to1go/central/static"
	"github.com/3to1go/shared/webui"
	"sync"
)

var pageShell = sync.OnceValues(func() ([]byte, error) { return webui.Index(static.Files) })

func readStaticFile(name string) ([]byte, error) {
	if name == "index.html" {
		return pageShell()
	}
	return static.Files.ReadFile(name)
}
