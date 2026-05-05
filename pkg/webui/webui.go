// Package webui is a fully backend-driven, read-only HTML browser for
// soft-serve repositories.
//
// It is intentionally separate from the rest of the codebase: it depends
// only on the repobrowser port (defined in repobrowser/) and on stdlib +
// gorilla/mux (already required by pkg/web). Templates, CSS and HTMX are
// the only client-side surface.
//
// Per AGENTS.md, this package never imports a concrete adapter. The
// composition root constructs a repobrowser.Browser implementation and
// passes it to NewHandler.
package webui

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/charmbracelet/soft-serve/pkg/webui/repobrowser"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Option configures a Handler.
type Option func(*Handler)

// WithBasePath sets the URL prefix the UI is mounted under (e.g. "/ui").
// Default is "" (mounted at root).
func WithBasePath(p string) Option {
	return func(h *Handler) { h.basePath = p }
}

// MaxBlobBytes sets the cap on rendered blob size. Default 256KiB.
func MaxBlobBytes(n int64) Option {
	return func(h *Handler) { h.maxBlobBytes = n }
}

// NewHandler returns an http.Handler serving the read-only browser.
//
// The handler is self-contained: it owns its routes, templates and static
// assets. It does not register itself on any external router; callers
// mount it (typically at "/" or "/ui") via http.Handle / mux.Handle.
func NewHandler(b repobrowser.Browser, opts ...Option) (http.Handler, error) {
	if b == nil {
		return nil, fmt.Errorf("webui: nil Browser")
	}
	h := &Handler{
		browser:      b,
		basePath:     "",
		maxBlobBytes: 256 * 1024,
	}
	for _, opt := range opts {
		opt(h)
	}

	tmpls, err := loadTemplates(templatesFS)
	if err != nil {
		return nil, fmt.Errorf("webui: load templates: %w", err)
	}
	h.tmpls = tmpls

	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("webui: static sub: %w", err)
	}
	h.staticFS = staticSub

	return h.routes(), nil
}
