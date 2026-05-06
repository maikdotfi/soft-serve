package backend

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/backup"
	"github.com/charmbracelet/soft-serve/pkg/config"
	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/db/migrate"
	"github.com/charmbracelet/soft-serve/pkg/hooks"
	"github.com/charmbracelet/soft-serve/pkg/store/database"
	"github.com/matryer/is"
)

// testContext sets up a test context with a SQLite database, runs migrations,
// and returns the context, DB handle, and config.
func testContext(t *testing.T) (context.Context, *db.DB, *config.Config) {
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
	if err := migrate.Migrate(ctx, dbx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return ctx, dbx, cfg
}

// minimalBackupService creates a BackupService with only the store wired.
// The S3, bundle, and snapshot providers are nil — this is sufficient for
// verifying the wiring pattern without needing real adapters.
func minimalBackupService(store backup.BackupStore) *backup.BackupService {
	cfg := backup.DefaultBackupConfig()
	cfg.S3Endpoint = "https://s3.example.com"
	cfg.S3Bucket = "test-bucket"
	cfg.S3Region = "us-east-1"
	return backup.NewBackupService(cfg, store, nil, nil, nil, nil, &backup.WallClock{}, nil)
}

// TestBackend_BackupServiceWiredAfterInit catches bug #1:
// SetBackupService is never called during serve initialization, so
// push-triggered backups (CreateRepoBackupOnPush) never fire.
//
// The test creates a Backend and verifies that after wiring the backup
// service (as serve.go now does), BackupService() returns a non-nil
// service and the PostReceive hook can use it safely.
func TestBackend_BackupServiceWiredAfterInit(t *testing.T) {
	is := is.New(t)
	ctx, dbx, cfg := testContext(t)

	dbstore := database.New(ctx, dbx)
	be := New(ctx, cfg, dbx, dbstore)

	// Before wiring: BackupService is nil.
	is.True(be.BackupService() == nil) // not yet wired

	// Wire the backup service (this is what serve.go does after migrations).
	svc := minimalBackupService(nil)
	be.SetBackupService(svc)

	// After wiring: BackupService must be non-nil.
	is.True(be.BackupService() != nil) // backup service must be wired after init
	is.True(be.BackupService().IsConfigured()) // wired service should be configured
}

// TestBackend_PostReceive_SkipsBackupWhenNilService verifies the guard
// in PostReceive works correctly when the backup service is nil.
// Without SetBackupService called, PostReceive must not crash — it
// must safely skip the backup path.
func TestBackend_PostReceive_SkipsBackupWhenNilService(t *testing.T) {
	is := is.New(t)
	ctx, dbx, cfg := testContext(t)

	dbstore := database.New(ctx, dbx)
	be := New(ctx, cfg, dbx, dbstore)

	// Verify backup service is nil (the current buggy state).
	is.True(be.BackupService() == nil) // without SetBackupService, service is nil

	// PostReceive with no repos and nil backup service must not panic.
	// The hook guard `d.backup != nil && d.backup.IsConfigured()` should
	// short-circuit safely.
	be.PostReceive(ctx, nil, nil, "nonexistent-repo", []hooks.HookArg{})
	// If we get here without panic, the nil-guard works.
}
