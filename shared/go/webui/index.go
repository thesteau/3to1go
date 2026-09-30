// Package webui assembles the static application shell without fetching data.
package webui

import (
	"bytes"
	"errors"
	"io/fs"
	"sync"
)

// Index includes dialog markup in the initial document, avoiding a startup
// waterfall of fragment requests before authentication and panel requests.
// Stylesheets are inlined at the app-styles placeholder in the supplied order.
func Index(files fs.FS, stylesheets ...string) ([]byte, error) {
	page, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return nil, err
	}
	stylesMarker := []byte("<!-- app-styles -->")
	if !bytes.Contains(page, stylesMarker) {
		return nil, errors.New("index.html missing <!-- app-styles --> placeholder")
	}
	// Ship styles with the shell so its first paint needs no extra requests.
	var styles bytes.Buffer
	for _, name := range stylesheets {
		css, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, err
		}
		styles.WriteString("<style>")
		styles.Write(css)
		styles.WriteString("</style>")
	}
	page = bytes.Replace(page, stylesMarker, styles.Bytes(), 1)
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

// Reader caches the assembled index and reads other assets from files.
func Reader(files fs.FS, stylesheets ...string) func(string) ([]byte, error) {
	page := sync.OnceValues(func() ([]byte, error) { return Index(files, stylesheets...) })
	return func(name string) ([]byte, error) {
		if name == "index.html" {
			return page()
		}
		return fs.ReadFile(files, name)
	}
}
