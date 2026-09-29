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
	"github.com/charmbracelet/soft-serve/pkg/webhook"
	"github.com/matryer/is"
)

// TestWireOptionalServices_WiresCIOnly pins the post-refactor contract:
// WireOptionalServices is the helper both the serve process and the hook
// subprocess call to attach CI to a freshly constructed Backend. It must
// NOT touch the backup service — backup is schedule-only and lives only
// in serve, attached via WireBackupService.
func TestWireOptionalServices_WiresCIOnly(t *testing.T) {
	is := is.New(t)
	ctx, dbx, cfg, st := newWiringTestContext(t, true)

	be := backend.New(ctx, cfg, dbx, st)
	is.True(be.BackupService() == nil) // sanity: not wired before the helper
	is.True(be.CIService() == nil)     // sanity: not wired before the helper

	is.NoErr(WireOptionalServices(ctx, cfg, be, dbx, st))

	is.True(be.CIService() != nil)     // CI is wired unconditionally
	is.True(be.BackupService() == nil) // backup is NOT wired by this helper
}

// TestWireBackupService_Enabled pins the serve-only backup wiring path.
func TestWireBackupService_Enabled(t *testing.T) {
	is := is.New(t)
	ctx, dbx, cfg, st := newWiringTestContext(t, true)

	be := backend.New(ctx, cfg, dbx, st)
	is.True(be.BackupService() == nil) // sanity: not wired before the helper

	WireBackupService(ctx, cfg, be, dbx, st)

	is.True(be.BackupService() != nil)         // backup service must be wired
	is.True(be.BackupService().IsConfigured()) // and configured
}

// TestWireBackupService_Disabled verifies the backup wiring helper leaves
// the backup service nil when backup is not enabled.
func TestWireBackupService_Disabled(t *testing.T) {
	is := is.New(t)
	ctx, dbx, cfg, st := newWiringTestContext(t, false)

	be := backend.New(ctx, cfg, dbx, st)

	WireBackupService(ctx, cfg, be, dbx, st)

	is.True(be.BackupService() == nil) // backup stays nil when disabled
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

// newServiceContext puts everything the context-driven wiring entry points
// read into ctx, as serve and hook do before calling them.
func newServiceContext(t *testing.T, backupEnabled bool) (context.Context, *backend.Backend) {
	t.Helper()
	ctx, dbx, cfg, st := newWiringTestContext(t, backupEnabled)
	be := backend.New(ctx, cfg, dbx, st)
	ctx = db.WithContext(ctx, dbx)
	ctx = store.WithContext(ctx, st)
	ctx = backend.WithContext(ctx, be)
	return ctx, be
}

// TestWireHookServices_WiresCIOnly pins what the hook subprocess gets.
func TestWireHookServices_WiresCIOnly(t *testing.T) {
	is := is.New(t)
	ctx, be := newServiceContext(t, true)

	is.NoErr(WireHookServices(ctx))

	is.True(be.CIService() != nil)     // pre-receive validation needs CI
	is.True(be.BackupService() == nil) // backup never runs in the hook
}

// TestWireServeServices_WiresCIBackupAndSchedule pins the serve process wiring.
func TestWireServeServices_WiresCIBackupAndSchedule(t *testing.T) {
	is := is.New(t)
	ctx, be := newServiceContext(t, true)
	t.Cleanup(webhook.ClearFiredEventHandler)

	is.NoErr(WireServeServices(ctx))

	is.True(be.CIService() != nil)
	is.True(be.BackupService() != nil)
	_, err := store.FromContext(ctx).GetBackupSchedule(ctx, db.FromContext(ctx))
	is.NoErr(err) // the default schedule is created at startup
}

// TestWireServeServices_RoutesFiredWebhooksToBackend verifies serve replaces
// any previous in-process webhook subscriber with the backend's.
func TestWireServeServices_RoutesFiredWebhooksToBackend(t *testing.T) {
	is := is.New(t)
	ctx, _ := newServiceContext(t, false)
	t.Cleanup(webhook.ClearFiredEventHandler)

	sentinelCalled := false
	webhook.SetFiredEventHandler(func(context.Context, webhook.EventPayload) { sentinelCalled = true })

	is.NoErr(WireServeServices(ctx))
	is.NoErr(webhook.SendEvent(ctx, webhook.Common{EventType: webhook.EventRepository}))

	is.True(!sentinelCalled) // serve's handler must replace the previous one
}
