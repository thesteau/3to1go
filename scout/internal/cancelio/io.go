// Package cancelio checks cancellation between streaming I/O operations.
package cancelio

import (
	"context"
	"io"
)

type Reader struct {
	Context context.Context
	Reader  io.Reader
}

func (r Reader) Read(p []byte) (int, error) {
	if err := r.Context.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

type Writer struct {
	Context context.Context
	Writer  io.Writer
}

func (w Writer) Write(p []byte) (int, error) {
	if err := w.Context.Err(); err != nil {
		return 0, err
	}
	return w.Writer.Write(p)
}
