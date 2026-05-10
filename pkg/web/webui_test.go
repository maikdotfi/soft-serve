package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/backup"
	backupstoreadapter "github.com/charmbracelet/soft-serve/pkg/backup/adapters/store"
	"github.com/charmbracelet/soft-serve/pkg/config"
	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/db/migrate"
	"github.com/charmbracelet/soft-serve/pkg/store"
	"github.com/charmbracelet/soft-serve/pkg/store/database"
	"github.com/gorilla/mux"
)

func TestWebUIController_MountsBackupStatusFromStore(t *testing.T) {
	ctx, dbx, dbstore := newWebUITestContext(t)
	be := backend.New(ctx, config.FromContext(ctx), dbx, dbstore)
	ctx = backend.WithContext(ctx, be)

	backups := backupstoreadapter.NewStoreAdapter(dbx, dbstore)
	failed, err := backups.CreateRepoBackup(ctx, "alpha", time.Date(2026, 4, 2, 11, 15, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateRepoBackup: %v", err)
	}
	if err := backups.UpdateRepoBackupStatus(ctx, failed.ID, backup.RepoBackupFailed, 3); err != nil {
		t.Fatalf("UpdateRepoBackupStatus: %v", err)
	}

	router := mux.NewRouter()
	WebUIController(ctx, router)
	wrapped := withRequestContext(ctx, router)

	req := httptest.NewRequest(http.MethodGet, "/ui/backups", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body:\n%s", rec.Code, body)
	}
	for _, want := range []string{"Backups", "repo backup", "alpha", "failed", "2026-04-02 11:15 UTC"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

func newWebUITestContext(t *testing.T) (context.Context, *db.DB, store.Store) {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	cfg := &config.Config{
		DataPath: tmp,
		DB: config.DBConfig{
			Driver:     "sqlite",
			DataSource: filepath.Join(tmp, "test.db") + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)",
		},
	}

	dbx, err := db.Open(ctx, cfg.DB.Driver, cfg.DB.DataSource)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { dbx.Close() })

	ctx = config.WithContext(ctx, cfg)
	ctx = log.WithContext(ctx, log.New(io.Discard))
	ctx = db.WithContext(ctx, dbx)
	if err := migrate.Migrate(ctx, dbx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	dbstore := database.New(ctx, dbx)
	ctx = store.WithContext(ctx, dbstore)
	return ctx, dbx, dbstore
}

func withRequestContext(ctx context.Context, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}
