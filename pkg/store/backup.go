package store

import (
	"context"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/db/models"
)

// BackupStore is the interface for persisting backup domain entities.
type BackupStore interface {
	// BackupSchedule operations
	GetBackupSchedule(ctx context.Context, h db.Handler) (models.BackupSchedule, error)
	SetBackupScheduleNextRunAt(ctx context.Context, h db.Handler, nextRunAt time.Time) error

	// RepoBackup operations
	CreateRepoBackup(ctx context.Context, h db.Handler, repoName string, createdAt time.Time) (models.RepoBackup, error)
	GetRepoBackup(ctx context.Context, h db.Handler, id int64) (models.RepoBackup, error)
	ListRepoBackupsByRepo(ctx context.Context, h db.Handler, repoName string) ([]models.RepoBackup, error)
	ListRepoBackupsByStatus(ctx context.Context, h db.Handler, status string) ([]models.RepoBackup, error)
	UpdateRepoBackupStatus(ctx context.Context, h db.Handler, id int64, status string, retryCount int) error
	MarkRepoBackupsTimedOut(ctx context.Context, h db.Handler, cutoff time.Time) (int64, error)
	DeleteRepoBackup(ctx context.Context, h db.Handler, id int64) error

	// ServerSnapshot operations
	CreateServerSnapshot(ctx context.Context, h db.Handler, createdAt time.Time) (models.ServerSnapshot, error)
	GetServerSnapshot(ctx context.Context, h db.Handler, id int64) (models.ServerSnapshot, error)
	ListServerSnapshotsByStatus(ctx context.Context, h db.Handler, status string) ([]models.ServerSnapshot, error)
	UpdateServerSnapshotStatus(ctx context.Context, h db.Handler, id int64, status string, retryCount int) error
	MarkServerSnapshotsTimedOut(ctx context.Context, h db.Handler, cutoff time.Time) (int64, error)
	DeleteServerSnapshot(ctx context.Context, h db.Handler, id int64) error

	// RestoreJob operations
	CreateRestoreJob(ctx context.Context, h db.Handler) (models.RestoreJob, error)
	GetRestoreJob(ctx context.Context, h db.Handler, id int64) (models.RestoreJob, error)
	ListRestoreJobsByStatus(ctx context.Context, h db.Handler, status string) ([]models.RestoreJob, error)
	UpdateRestoreJobStatus(ctx context.Context, h db.Handler, id int64, status string) error
}