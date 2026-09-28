// Package webui assembles the static application shell without fetching data.
package webui

import (
	"bytes"
	"io/fs"
	"regexp"
)

var stylesheet = regexp.MustCompile(`<link rel="stylesheet" href="/static/(css/[^"]+)">`)

// Index includes dialog markup in the initial document, avoiding a startup
// waterfall of fragment requests before authentication and panel requests.
func Index(files fs.FS) ([]byte, error) {
	page, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return nil, err
	}
	// Ship styles with the shell so its first paint needs no extra requests.
	for _, match := range stylesheet.FindAllSubmatch(page, -1) {
		css, err := fs.ReadFile(files, string(match[1]))
		if err != nil {
			return nil, err
		}
		style := append([]byte("<style>"), css...)
		style = append(style, []byte("</style>")...)
		page = bytes.Replace(page, match[0], style, 1)
	}
	names, err := fs.Glob(files, "html/*-dialog.html")
	if err != nil {
		return nil, err
	}
	var dialogs bytes.Buffer
	for _, name := range names {
		fragment, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, err
		}
		dialogs.Write(fragment)
	}
	return bytes.Replace(page, []byte("<!-- app-dialogs -->"), dialogs.Bytes(), 1), nil
}
