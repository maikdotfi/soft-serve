package jobs

import (
	"context"
	"fmt"
	"time"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/backup"
	"github.com/charmbracelet/soft-serve/pkg/backup/adapters/bundle"
	ss3 "github.com/charmbracelet/soft-serve/pkg/backup/adapters/s3"
	"github.com/charmbracelet/soft-serve/pkg/backup/adapters/snapshot"
	storeadapter "github.com/charmbracelet/soft-serve/pkg/backup/adapters/store"
	"github.com/charmbracelet/soft-serve/pkg/config"
	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/store"
)

func init() {
	Register("backup", backupJob{})
}

// backupJob is the cron job that runs the backup schedule (FireBackupSchedule).
// Per spec rule FireBackupSchedule: when schedule.next_run_at <= now, fire the
// schedule and create backups/snapshots.
type backupJob struct{}

// Spec returns the cron spec for the backup job.
// The spec is derived from the backup schedule interval config.
func (j backupJob) Spec(ctx context.Context) string {
	cfg := config.FromContext(ctx)
	if !cfg.Backup.Enabled {
		return "" // disabled
	}
	// Run every minute to check if the schedule should fire
	// The actual interval is handled by the BackupService.Tick method
	return "@every 1m"
}

// Func returns the function that runs when the backup job fires.
func (j backupJob) Func(ctx context.Context) func() {
	cfg := config.FromContext(ctx)
	logger := log.FromContext(ctx).WithPrefix("jobs.backup")

	return func() {
		if !cfg.Backup.Enabled {
			return
		}

		be := backend.FromContext(ctx)
		dbx := db.FromContext(ctx)
		dbstore := store.FromContext(ctx)

		if dbx == nil || dbstore == nil {
			logger.Debug("backup job skipped: database not available")
			return
		}

		// Convert config to domain BackupConfig (avoids import cycle)
		cfgResult, err := config.ConvertBackupConfig(cfg.Backup)
		if err != nil {
			logger.Error("invalid backup configuration", "err", err)
			return
		}
		backupCfg := backup.BackupConfig{
			S3Endpoint:         cfgResult.S3Endpoint,
			S3Bucket:           cfgResult.S3Bucket,
			S3Region:           cfgResult.S3Region,
			S3PathPrefix:       cfgResult.S3PathPrefix,
			ScheduleInterval:   cfgResult.ScheduleInterval,
			MaxRepoBackups:     cfgResult.MaxRepoBackups,
			MaxServerSnapshots: cfgResult.MaxServerSnapshots,
			MaxUploadRetries:   cfgResult.MaxUploadRetries,
			UploadTimeout:      cfgResult.UploadTimeout,
		}

		// Check minimum configuration
		if cfg.Backup.S3Endpoint == "" || cfg.Backup.S3Bucket == "" || cfg.Backup.S3Region == "" {
			logger.Debug("backup job skipped: S3 not configured")
			return
		}

		// Create S3 adapter
		s3Adapter, err := ss3.NewAdapter(ss3.S3Config{
			Endpoint:   backupCfg.S3Endpoint,
			Region:     backupCfg.S3Region,
			Bucket:     backupCfg.S3Bucket,
			PathPrefix: backupCfg.S3PathPrefix,
			AccessKey:  cfg.Backup.S3AccessKey,
			SecretKey:  cfg.Backup.S3SecretKey,
		})
		if err != nil {
			logger.Error("failed to create S3 adapter", "err", err)
			return
		}

		// Create store adapter
		storeAdapter := storeadapter.NewStoreAdapter(dbx, dbstore)

		// Create providers
		bundleProvider := bundle.NewGitBundleProvider(cfg.DataPath)
		snapshotProvider := snapshot.NewServerSnapshotProvider(cfg.DataPath, dbx, cfg.DB.DataSource, dbstore)
		repoProvider := &backendRepoProvider{be: be}

		// Create backup service
		svc := backup.NewBackupService(
			backupCfg,
			storeAdapter,
			s3Adapter,
			bundleProvider,
			snapshotProvider,
			repoProvider,
			&backup.WallClock{},
			logger,
		)

		// Ensure default schedule exists
		if err := svc.CreateDefaultBackupSchedule(ctx); err != nil {
			logger.Error("failed to create default backup schedule", "err", err)
			return
		}

		// Check if it's time to fire the schedule and run backups
		if err := svc.Tick(ctx); err != nil {
			logger.Error("backup tick failed", "err", err)
		}

		// Enforce timeouts for stuck uploads
		if err := svc.EnforceTimeouts(ctx); err != nil {
			logger.Error("failed to enforce timeouts", "err", err)
		}
	}
}

// backendRepoProvider wraps the Backend to implement backup.RepoProvider.
type backendRepoProvider struct {
	be *backend.Backend
}

// ListRepos returns all repositories known to the server.
func (p *backendRepoProvider) ListRepos(ctx context.Context) ([]backup.RepoInfo, error) {
	repos, err := p.be.Repositories(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]backup.RepoInfo, len(repos))
	for i, r := range repos {
		defaultBranch := "main"
		if rr, err := r.Open(); err == nil {
			if head, err := rr.HEAD(); err == nil {
				defaultBranch = head.Name().Short()
			}
		}
		result[i] = backup.RepoInfo{
			Name:          r.Name(),
			DefaultBranch: defaultBranch,
		}
	}
	return result, nil
}

// formatDuration returns a human-readable duration string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}