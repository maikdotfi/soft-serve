package sqlstore_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/config"
	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/db/migrate"
	"github.com/charmbracelet/soft-serve/pkg/workitem/adapters/sqlstore"
	"github.com/charmbracelet/soft-serve/pkg/workitem/workitemtest"
)

func TestStore_Contract(t *testing.T) {
	ctx := config.WithContext(context.Background(), config.DefaultConfig())
	dbpath := filepath.Join(t.TempDir(), "workitem-sqlstore.db")
	dbx, err := db.Open(ctx, "sqlite", dbpath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if err := dbx.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	if err := migrate.Migrate(ctx, dbx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	seedRepo(t, ctx, dbx, "alpha")
	seedRepo(t, ctx, dbx, "beta")
	seedRepo(t, ctx, dbx, "ordered")
	seedRepo(t, ctx, dbx, "move")
	seedRepo(t, ctx, dbx, "message-alpha")
	seedRepo(t, ctx, dbx, "message-beta")

	workitemtest.RunStoreContract(t, sqlstore.New(dbx))
}

func seedRepo(t *testing.T, ctx context.Context, dbx *db.DB, name string) {
	t.Helper()
	query := dbx.Rebind(`INSERT INTO repos
		(name, project_name, description, private, mirror, hidden, user_id, updated_at)
		VALUES (?, ?, '', false, false, false, 1, CURRENT_TIMESTAMP);`)
	if _, err := dbx.ExecContext(ctx, query, name, name); err != nil {
		t.Fatalf("seed repo %q: %v", name, err)
	}
}
