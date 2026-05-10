package cmd

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/config"
	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/db/migrate"
	"github.com/charmbracelet/soft-serve/pkg/store"
	"github.com/charmbracelet/soft-serve/pkg/store/database"
	"github.com/matryer/is"
)

// TestWireOptionalServices_BackupEnabled is the regression test for the
// silent push-backup bug. The post-receive hook subprocess goes through
// InitBackendContext, which constructs a fresh Backend with no backup or CI
// service. Push-triggered backup (CreateRepoBackupOnPush) and CI workflow
// sync (WorkflowsSyncedOnPush) therefore never fired, with no log output
// because the hook guards (b.backup == nil, b.ci == nil) silently no-op.
//
// The fix wires the optional services in a helper that both the long-running
// serve process and the hook subprocess call. This test pins the contract:
// after WireOptionalServices, a Backend constructed in a hook-subprocess-
// equivalent path carries a non-nil backup service when backup is enabled
// in config.
func TestWireOptionalServices_BackupEnabled(t *testing.T) {
	is := is.New(t)
	ctx, dbx, cfg, st := newWiringTestContext(t, true)

	be := backend.New(ctx, cfg, dbx, st)
	is.True(be.BackupService() == nil) // sanity: not wired before the helper

	is.NoErr(WireOptionalServices(ctx, cfg, be, dbx, st))

	is.True(be.BackupService() != nil)         // backup service must be wired
	is.True(be.BackupService().IsConfigured()) // and configured
	is.True(be.CIService() != nil)             // CI is wired unconditionally
}

// TestWireOptionalServices_BackupDisabled verifies the helper leaves the
// backup service nil when backup is not enabled, so the PostReceive guard
// short-circuits cleanly. CI is still wired (it is unconditional in serve.go
// today and behaves the same here).
func TestWireOptionalServices_BackupDisabled(t *testing.T) {
	is := is.New(t)
	ctx, dbx, cfg, st := newWiringTestContext(t, false)

	be := backend.New(ctx, cfg, dbx, st)

	is.NoErr(WireOptionalServices(ctx, cfg, be, dbx, st))

	is.True(be.BackupService() == nil) // backup stays nil when disabled
	is.True(be.CIService() != nil)     // CI is wired regardless
}

// newWiringTestContext builds a sqlite-backed config + db + store suitable
// for exercising WireOptionalServices. backupEnabled toggles the
// backup-config knobs that gate the backup wiring branch.
func newWiringTestContext(t *testing.T, backupEnabled bool) (context.Context, *db.DB, *config.Config, store.Store) {
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
	if backupEnabled {
		cfg.Backup = config.DefaultS3BackupConfig()
		cfg.Backup.Enabled = true
		cfg.Backup.S3Endpoint = "https://s3.example.com"
		cfg.Backup.S3Bucket = "test-bucket"
		cfg.Backup.S3Region = "us-east-1"
	}
	ctx = config.WithContext(ctx, cfg)

	dbx, err := db.Open(ctx, cfg.DB.Driver, cfg.DB.DataSource)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { dbx.Close() })

	if err := migrate.Migrate(ctx, dbx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	st := database.New(ctx, dbx)
	return ctx, dbx, cfg, st
}
