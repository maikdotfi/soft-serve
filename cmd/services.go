package cmd

import (
	"context"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/backup"
	"github.com/charmbracelet/soft-serve/pkg/backup/adapters/bundle"
	ss3 "github.com/charmbracelet/soft-serve/pkg/backup/adapters/s3"
	"github.com/charmbracelet/soft-serve/pkg/backup/adapters/snapshot"
	storeadapter "github.com/charmbracelet/soft-serve/pkg/backup/adapters/store"
	"github.com/charmbracelet/soft-serve/pkg/ci"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/backendaccess"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/cryptotokens"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/httpdispatch"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/realclock"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/sqlstore"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/yamlworkflows"
	"github.com/charmbracelet/soft-serve/pkg/config"
	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/store"
)

// WireOptionalServices attaches the CI service to the Backend. It is
// called by every composition root that constructs a Backend and
// dispatches git hooks — both the long-running `soft serve` process and
// the short-lived `soft hook *` subprocess that git invokes on receive.
// CI is wired here because pre-receive workflow validation runs
// synchronously inside the hook subprocess.
//
// Backup is intentionally NOT wired here. Backup is schedule-only and
// runs entirely inside the long-running serve process; the hook
// subprocess has no business spawning S3 uploads. Serve calls
// WireBackupService directly.
func WireOptionalServices(ctx context.Context, cfg *config.Config, be *backend.Backend, dbx *db.DB, st store.Store) error {
	wireCIService(ctx, cfg, be, dbx)
	return nil
}

// WireBackupService constructs and attaches the backup service if
// cfg.Backup.Enabled is true and the minimum S3 settings are present.
// Only the long-running serve process (and the standalone restore
// command) need this; the hook subprocess does not.
//
// Failures are logged and downgraded to "backup disabled" rather than
// aborting startup; this matches the existing behavior in serve.go.
func WireBackupService(ctx context.Context, cfg *config.Config, be *backend.Backend, dbx *db.DB, st store.Store) {
	if !cfg.Backup.Enabled {
		return
	}
	logger := log.FromContext(ctx).WithPrefix("backup")

	cfgResult, err := config.ConvertBackupConfig(cfg.Backup)
	if err != nil {
		logger.Error("invalid backup configuration, scheduled backups disabled", "err", err)
		return
	}
	if cfg.Backup.S3Endpoint == "" || cfg.Backup.S3Bucket == "" || cfg.Backup.S3Region == "" {
		logger.Warn("backup enabled but S3 endpoint/bucket/region incomplete, scheduled backups disabled")
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

	s3Adapter, err := ss3.NewAdapter(ss3.S3Config{
		Endpoint:   backupCfg.S3Endpoint,
		Region:     backupCfg.S3Region,
		Bucket:     backupCfg.S3Bucket,
		PathPrefix: backupCfg.S3PathPrefix,
		AccessKey:  cfg.Backup.S3AccessKey,
		SecretKey:  cfg.Backup.S3SecretKey,
	})
	if err != nil {
		logger.Error("failed to create S3 adapter, scheduled backups disabled", "err", err)
		return
	}

	storeAdapter := storeadapter.NewStoreAdapter(dbx, st)
	bundler := bundle.NewGitBundleProvider(cfg.DataPath)
	snapshotProvider := snapshot.NewServerSnapshotProvider(cfg.DataPath, dbx, cfg.DB.DataSource, st)

	svc := backup.NewBackupService(
		backupCfg,
		storeAdapter,
		s3Adapter,
		bundler,
		snapshotProvider,
		&backendRepoProvider{be: be},
		&backup.WallClock{},
		logger,
	)
	be.SetBackupService(svc)
	logger.Info("backup service wired, scheduled backups enabled")
}

// wireCIService constructs and attaches the CI service. CI is wired
// unconditionally — see the comment in cmd/soft/serve/serve.go where this
// logic originally lived.
func wireCIService(ctx context.Context, cfg *config.Config, be *backend.Backend, dbx *db.DB) {
	logger := log.FromContext(ctx).WithPrefix("ci")
	svc := ci.NewService(
		ci.DefaultConfig(),
		sqlstore.New(dbx),
		yamlworkflows.New(be),
		httpdispatch.New(nil, cfg.HTTP.PublicURL),
		cryptotokens.New(),
		backendaccess.New(be),
		realclock.New(),
		logger,
	)
	be.SetCIService(svc)
	logger.Info("ci subsystem wired")
}

// backendRepoProvider adapts a Backend to backup.RepoProvider, shared by
// every composition root that wires the backup service.
type backendRepoProvider struct {
	be *backend.Backend
}

// ListRepos returns all repositories known to the backend. The default
// branch is resolved from the on-disk repo HEAD, falling back to "main"
// when the repo cannot be opened or has no HEAD.
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
