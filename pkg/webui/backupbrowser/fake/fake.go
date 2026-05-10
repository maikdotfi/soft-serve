// Package fake provides an in-memory backupbrowser.Reader for web UI tests.
package fake

import (
	"context"

	"github.com/charmbracelet/soft-serve/pkg/webui/backupbrowser"
)

// Reader returns a fixed backup overview.
type Reader struct {
	overview backupbrowser.Overview
	err      error
}

// New returns a Reader that serves overview.
func New(overview backupbrowser.Overview) *Reader {
	return &Reader{overview: overview}
}

// WithError returns a Reader that fails with err.
func WithError(err error) *Reader {
	return &Reader{err: err}
}

// Overview implements backupbrowser.Reader.
func (r *Reader) Overview(_ context.Context) (backupbrowser.Overview, error) {
	if r.err != nil {
		return backupbrowser.Overview{}, r.err
	}
	return r.overview, nil
}

var _ backupbrowser.Reader = (*Reader)(nil)
