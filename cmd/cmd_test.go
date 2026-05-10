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
