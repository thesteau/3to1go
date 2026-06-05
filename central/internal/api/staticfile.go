package api

import (
	"github.com/relay/central/static"
)

func readStaticFile(name string) ([]byte, error) {
	return static.Files.ReadFile(name)
}
