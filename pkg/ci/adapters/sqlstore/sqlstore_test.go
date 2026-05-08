package sqlstore_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/sqlstore"
	"github.com/charmbracelet/soft-serve/pkg/ci/citest"
	"github.com/charmbracelet/soft-serve/pkg/config"
	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/db/migrate"
)

func TestSQLStore_Contract(t *testing.T) {
	// Migrations expect a config in context (see migrate_test.go).
	ctx := config.WithContext(context.Background(), config.DefaultConfig())
	dbpath := filepath.Join(t.TempDir(), "sqlstore.db")
	dbx, err := db.Open(ctx, "sqlite", dbpath)
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

	citest.RunStoreContract(t, sqlstore.New(dbx))
}
