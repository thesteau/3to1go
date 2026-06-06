package api

import (
	"github.com/3to1go/central/static"
)

func readStaticFile(name string) ([]byte, error) {
	return static.Files.ReadFile(name)
}
