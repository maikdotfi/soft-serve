// Package restore provides the `soft restore` command for restoring
// Soft Serve from an S3 backup. Per spec surface AdminBackupManagement:
// "Admin triggers a full restore via a subcommand (e.g., soft restore)."
package restore

import (
	"context"
	"fmt"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/backup"
	"github.com/charmbracelet/soft-serve/pkg/backup/adapters/bundle"
	ss3 "github.com/charmbracelet/soft-serve/pkg/backup/adapters/s3"
	"github.com/charmbracelet/soft-serve/pkg/backup/adapters/snapshot"
	storeadapter "github.com/charmbracelet/soft-serve/pkg/backup/adapters/store"
	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/config"
	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/store"
	"github.com/charmbracelet/soft-serve/pkg/store/database"
	"github.com/spf13/cobra"
)

var (
	// Command is the restore command.
	Command = &cobra.Command{
		Use:   "restore",
		Short: "Restore the server from an S3 backup",
		Long:  "Restore Soft Serve server data and repositories from an S3 backup. The server must be able to reach the S3 bucket where backups are stored.",
		PersistentPreRunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			cfg := config.FromContext(ctx)

			// Check backup is configured
			if !cfg.Backup.Enabled {
				return fmt.Errorf("S3 backup is not enabled. Set SOFT_SERVE_BACKUP_ENABLED=true to enable it")
			}

			if cfg.Backup.S3Endpoint == "" || cfg.Backup.S3Bucket == "" || cfg.Backup.S3Region == "" {
				return fmt.Errorf("S3 backup is not configured. Set SOFT_SERVE_BACKUP_ENDPOINT, SOFT_SERVE_BACKUP_BUCKET, and SOFT_SERVE_BACKUP_REGION")
			}

			return nil
		},
		RunE: runRestore,
	}

	force bool
)

func init() {
	Command.Flags().BoolVarP(&force, "force", "f", false, "force restore even if there are existing restore jobs in progress")
}

func runRestore(c *cobra.Command, _ []string) error {
	ctx := c.Context()
	cfg := config.FromContext(ctx)
	logger := log.FromContext(ctx).WithPrefix("restore")

	// Validate configuration
	cfgResult, err := config.ConvertBackupConfig(cfg.Backup)
	if err != nil {
		return fmt.Errorf("invalid backup configuration: %w", err)
	}
	backupCfg := backup.BackupConfig{
		S3Endpoint:           cfgResult.S3Endpoint,
		S3Bucket:             cfgResult.S3Bucket,
		S3Region:             cfgResult.S3Region,
		S3PathPrefix:          cfgResult.S3PathPrefix,
		ScheduleInterval:     cfgResult.ScheduleInterval,
		MaxRepoBackups:       cfgResult.MaxRepoBackups,
		MaxServerSnapshots:    cfgResult.MaxServerSnapshots,
		MaxUploadRetries:      cfgResult.MaxUploadRetries,
		UploadTimeout:         cfgResult.UploadTimeout,
		BackupReposOnSchedule: cfgResult.BackupReposOnSchedule,
	}

	// Open database
	dbx, err := db.Open(ctx, cfg.DB.Driver, cfg.DB.DataSource)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer dbx.Close() //nolint:errcheck

	ctx = db.WithContext(ctx, dbx)
	dbstore := database.New(ctx, dbx)
	ctx = store.WithContext(ctx, dbstore)
	be := backend.New(ctx, cfg, dbx, dbstore)
	ctx = backend.WithContext(ctx, be)

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
		return fmt.Errorf("creating S3 adapter: %w", err)
	}

	// Ensure bucket exists
	if err := s3Adapter.EnsureBucket(ctx); err != nil {
		return fmt.Errorf("ensuring S3 bucket exists: %w", err)
	}

	// Create store adapter
	sa := storeadapter.NewStoreAdapter(dbx, dbstore)

	// Create bundle provider
	bundleProvider := bundle.NewGitBundleProvider(cfg.DataPath)

	// Create snapshot provider
	snapshotProvider := snapshot.NewServerSnapshotProvider(cfg.DataPath, dbx, cfg.DB.DataSource, dbstore)

	// Create repo provider wrapper
	repoProvider := &backendRepoProvider{be: be}

	// Create backup service
	svc := backup.NewBackupService(
		backupCfg,
		sa,
		s3Adapter,
		bundleProvider,
		snapshotProvider,
		repoProvider,
		&backup.WallClock{},
		logger,
	)

	// Check for existing in-progress restore jobs
	if !force {
		activeJobs, err := svc.ListActiveRestoreJobs(ctx)
		if err != nil {
			return fmt.Errorf("checking for active restore jobs: %w", err)
		}
		if len(activeJobs) > 0 {
			return fmt.Errorf("there are %d active restore job(s). Use --force to override", len(activeJobs))
		}
	}

	// Start the restore
	adminUser := backup.UserInfo{Role: "admin"}
	job, err := svc.StartRestore(ctx, adminUser)
	if err != nil {
		return fmt.Errorf("starting restore: %w", err)
	}

	logger.Info("restore job started", "id", job.ID)
	fmt.Printf("Restore job %d started. The server will be restored from the latest backup.\n", job.ID)
	return nil
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
		defaultBranch := "main" // fallback
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