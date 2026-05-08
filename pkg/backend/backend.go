package backend

import (
	"context"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/backup"
	"github.com/charmbracelet/soft-serve/pkg/ci"
	"github.com/charmbracelet/soft-serve/pkg/config"
	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/store"
	"github.com/charmbracelet/soft-serve/pkg/task"
)

// Backend is the Soft Serve backend that handles users, repositories, and
// server settings management and operations.
type Backend struct {
	ctx     context.Context
	cfg     *config.Config
	db      *db.DB
	store   store.Store
	logger  *log.Logger
	cache   *cache
	manager *task.Manager
	backup  *backup.BackupService
	ci      *ci.Service
}

// New returns a new Soft Serve backend.
func New(ctx context.Context, cfg *config.Config, db *db.DB, st store.Store) *Backend {
	logger := log.FromContext(ctx).WithPrefix("backend")
	b := &Backend{
		ctx:     ctx,
		cfg:     cfg,
		db:      db,
		store:   st,
		logger:  logger,
		manager: task.NewManager(ctx),
	}

	// TODO: implement a proper caching interface
	cache := newCache(b, 1000)
	b.cache = cache

	return b
}

// SetBackupService sets the backup service on the backend.
func (b *Backend) SetBackupService(svc *backup.BackupService) {
	b.backup = svc
}

// BackupService returns the backup service.
func (b *Backend) BackupService() *backup.BackupService {
	return b.backup
}

// SetCIService sets the CI service on the backend. Until this is
// called CIService() returns nil and the push and webhook hooks no-op
// the CI integration.
func (b *Backend) SetCIService(svc *ci.Service) {
	b.ci = svc
}

// CIService returns the CI service, or nil if not configured.
func (b *Backend) CIService() *ci.Service {
	return b.ci
}
