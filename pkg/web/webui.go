package web

import (
	"context"
	"net/http"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/backend"
	backupstoreadapter "github.com/charmbracelet/soft-serve/pkg/backup/adapters/store"
	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/store"
	"github.com/charmbracelet/soft-serve/pkg/webui"
	webuibackupstore "github.com/charmbracelet/soft-serve/pkg/webui/backupbrowser/storeadapter"
	"github.com/charmbracelet/soft-serve/pkg/webui/repobrowser/softserveadapter"
	workitemserviceadapter "github.com/charmbracelet/soft-serve/pkg/webui/workitembrowser/serviceadapter"
	"github.com/gorilla/mux"
)

// WebUIController mounts the HTML browser at /ui.
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
	opts := []webui.Option{webui.WithBasePath("/ui")}
	if dbx, datastore := db.FromContext(ctx), store.FromContext(ctx); dbx != nil && datastore != nil {
		backupStore := backupstoreadapter.NewStoreAdapter(dbx, datastore)
		opts = append(opts, webui.WithBackupReader(webuibackupstore.New(backupStore)))
	}
	if be.WorkItemService() != nil {
		opts = append(opts, webui.WithWorkItemReader(workitemserviceadapter.New(be.WorkItemService())))
	}
	handler, err := webui.NewHandler(browser, opts...)
	if err != nil {
		log.FromContext(ctx).Error("webui: failed to construct handler", "err", err)
		return
	}

	r.PathPrefix("/ui/").Handler(http.StripPrefix("/ui", handler))
	r.Handle("/ui", http.RedirectHandler("/ui/", http.StatusFound))
}
