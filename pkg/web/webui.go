package web

import (
	"context"
	"net/http"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/webui"
	"github.com/charmbracelet/soft-serve/pkg/webui/repobrowser/softserveadapter"
	"github.com/gorilla/mux"
)

// WebUIController mounts the read-only HTML browser at /ui.
//
// The UI is intentionally separate from the rest of the HTTP server: the
// only seam is the repobrowser.Browser port, constructed here from the
// existing backend at the composition root.
func WebUIController(ctx context.Context, r *mux.Router) {
	be := backend.FromContext(ctx)
	if be == nil {
		log.FromContext(ctx).Warn("webui: no backend in context, skipping mount")
		return
	}

	browser := softserveadapter.New(be)
	handler, err := webui.NewHandler(browser, webui.WithBasePath("/ui"))
	if err != nil {
		log.FromContext(ctx).Error("webui: failed to construct handler", "err", err)
		return
	}

	r.PathPrefix("/ui/").Handler(http.StripPrefix("/ui", handler))
	r.Handle("/ui", http.RedirectHandler("/ui/", http.StatusFound))
}
