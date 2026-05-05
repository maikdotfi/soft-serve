// Package storeadapter provides an adapter that bridges the domain BackupStore
// interface to the database store interface.
// Per AGENTS.md: this is separated from the domain package to avoid import cycles.
package storeadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/backup"
	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/db/models"
	"github.com/charmbracelet/soft-serve/pkg/store"
)

// StoreAdapter bridges the domain BackupStore interface to the database
// BackupStore interface.
type StoreAdapter struct {
	store store.BackupStore
	db    *db.DB
}

// NewStoreAdapter creates a new StoreAdapter that wraps the database store.
func NewStoreAdapter(database *db.DB, s store.BackupStore) *StoreAdapter {
	return &StoreAdapter{
		store: s,
		db:    database,
	}
}

// domainRepoBackup converts a database model to a domain entity.
func domainRepoBackup(m models.RepoBackup) backup.RepoBackup {
	return backup.RepoBackup{
		ID:         m.ID,
		RepoName:   m.RepoName,
		CreatedAt:  m.CreatedAt,
		RetryCount: m.RetryCount,
		Status:     backup.RepoBackupStatus(m.Status),
	}
}

// domainServerSnapshot converts a database model to a domain entity.
func domainServerSnapshot(m models.ServerSnapshot) backup.ServerSnapshot {
	return backup.ServerSnapshot{
		ID:         m.ID,
		CreatedAt:  m.CreatedAt,
		RetryCount: m.RetryCount,
		Status:     backup.ServerSnapshotStatus(m.Status),
	}
}

// domainRestoreJob converts a database model to a domain entity.
func domainRestoreJob(m models.RestoreJob) backup.RestoreJob {
	return backup.RestoreJob{
		ID:     m.ID,
		Status: backup.RestoreJobStatus(m.Status),
	}
}

// GetBackupSchedule returns the backup schedule.
func (a *StoreAdapter) GetBackupSchedule(ctx context.Context) (*backup.BackupSchedule, error) {
	var schedule models.BackupSchedule
	err := a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		var err error
		schedule, err = a.store.GetBackupSchedule(ctx, tx)
		return db.WrapError(err)
	})
	if err != nil {
		return nil, backup.ErrBackupNotFound
	}
	result := backup.BackupSchedule{
		NextRunAt: schedule.NextRunAt,
	}
	return &result, nil
}

// SetBackupScheduleNextRunAt sets the next run time for the backup schedule.
func (a *StoreAdapter) SetBackupScheduleNextRunAt(ctx context.Context, nextRunAt time.Time) error {
	return a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		return a.store.SetBackupScheduleNextRunAt(ctx, tx, nextRunAt)
	})
}

// CreateRepoBackup creates a new repo backup record.
func (a *StoreAdapter) CreateRepoBackup(ctx context.Context, repoName string, createdAt time.Time) (*backup.RepoBackup, error) {
	var m models.RepoBackup
	err := a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		var err error
		m, err = a.store.CreateRepoBackup(ctx, tx, repoName, createdAt)
		return db.WrapError(err)
	})
	if err != nil {
		return nil, fmt.Errorf("creating repo backup: %w", err)
	}
	result := domainRepoBackup(m)
	return &result, nil
}

// GetRepoBackup returns a repo backup by ID.
func (a *StoreAdapter) GetRepoBackup(ctx context.Context, id int64) (*backup.RepoBackup, error) {
	var m models.RepoBackup
	err := a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		var err error
		m, err = a.store.GetRepoBackup(ctx, tx, id)
		return db.WrapError(err)
	})
	if err != nil {
		return nil, backup.ErrBackupNotFound
	}
	result := domainRepoBackup(m)
	return &result, nil
}

// ListRepoBackupsByRepo returns all repo backups for a given repo name.
func (a *StoreAdapter) ListRepoBackupsByRepo(ctx context.Context, repoName string) ([]backup.RepoBackup, error) {
	var models_ []models.RepoBackup
	err := a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		var err error
		models_, err = a.store.ListRepoBackupsByRepo(ctx, tx, repoName)
		return db.WrapError(err)
	})
	if err != nil {
		return nil, fmt.Errorf("listing repo backups: %w", err)
	}
	result := make([]backup.RepoBackup, len(models_))
	for i, m := range models_ {
		result[i] = domainRepoBackup(m)
	}
	return result, nil
}

// ListRepoBackupsByStatus returns all repo backups with the given status.
func (a *StoreAdapter) ListRepoBackupsByStatus(ctx context.Context, status backup.RepoBackupStatus) ([]backup.RepoBackup, error) {
	var models_ []models.RepoBackup
	err := a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		var err error
		models_, err = a.store.ListRepoBackupsByStatus(ctx, tx, string(status))
		return db.WrapError(err)
	})
	if err != nil {
		return nil, fmt.Errorf("listing repo backups by status: %w", err)
	}
	result := make([]backup.RepoBackup, len(models_))
	for i, m := range models_ {
		result[i] = domainRepoBackup(m)
	}
	return result, nil
}

// UpdateRepoBackupStatus updates a repo backup's status and retry count.
func (a *StoreAdapter) UpdateRepoBackupStatus(ctx context.Context, id int64, status backup.RepoBackupStatus, retryCount int) error {
	return a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		return a.store.UpdateRepoBackupStatus(ctx, tx, id, string(status), retryCount)
	})
}

// MarkRepoBackupsTimedOut marks all uploading repo backups older than cutoff as failed.
func (a *StoreAdapter) MarkRepoBackupsTimedOut(ctx context.Context, cutoff time.Time) (int64, error) {
	var affected int64
	err := a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		var err error
		affected, err = a.store.MarkRepoBackupsTimedOut(ctx, tx, cutoff)
		return err
	})
	return affected, err
}

// DeleteRepoBackup deletes a repo backup record.
func (a *StoreAdapter) DeleteRepoBackup(ctx context.Context, id int64) error {
	return a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		return a.store.DeleteRepoBackup(ctx, tx, id)
	})
}

// CreateServerSnapshot creates a new server snapshot record.
func (a *StoreAdapter) CreateServerSnapshot(ctx context.Context, createdAt time.Time) (*backup.ServerSnapshot, error) {
	var m models.ServerSnapshot
	err := a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		var err error
		m, err = a.store.CreateServerSnapshot(ctx, tx, createdAt)
		return db.WrapError(err)
	})
	if err != nil {
		return nil, fmt.Errorf("creating server snapshot: %w", err)
	}
	result := domainServerSnapshot(m)
	return &result, nil
}

// GetServerSnapshot returns a server snapshot by ID.
func (a *StoreAdapter) GetServerSnapshot(ctx context.Context, id int64) (*backup.ServerSnapshot, error) {
	var m models.ServerSnapshot
	err := a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		var err error
		m, err = a.store.GetServerSnapshot(ctx, tx, id)
		return db.WrapError(err)
	})
	if err != nil {
		return nil, backup.ErrSnapshotNotFound
	}
	result := domainServerSnapshot(m)
	return &result, nil
}

// ListServerSnapshotsByStatus returns all server snapshots with the given status.
func (a *StoreAdapter) ListServerSnapshotsByStatus(ctx context.Context, status backup.ServerSnapshotStatus) ([]backup.ServerSnapshot, error) {
	var models_ []models.ServerSnapshot
	err := a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		var err error
		models_, err = a.store.ListServerSnapshotsByStatus(ctx, tx, string(status))
		return db.WrapError(err)
	})
	if err != nil {
		return nil, fmt.Errorf("listing server snapshots by status: %w", err)
	}
	result := make([]backup.ServerSnapshot, len(models_))
	for i, m := range models_ {
		result[i] = domainServerSnapshot(m)
	}
	return result, nil
}

// UpdateServerSnapshotStatus updates a server snapshot's status and retry count.
func (a *StoreAdapter) UpdateServerSnapshotStatus(ctx context.Context, id int64, status backup.ServerSnapshotStatus, retryCount int) error {
	return a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		return a.store.UpdateServerSnapshotStatus(ctx, tx, id, string(status), retryCount)
	})
}

// MarkServerSnapshotsTimedOut marks all uploading server snapshots older than cutoff as failed.
func (a *StoreAdapter) MarkServerSnapshotsTimedOut(ctx context.Context, cutoff time.Time) (int64, error) {
	var affected int64
	err := a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		var err error
		affected, err = a.store.MarkServerSnapshotsTimedOut(ctx, tx, cutoff)
		return err
	})
	return affected, err
}

// DeleteServerSnapshot deletes a server snapshot record.
func (a *StoreAdapter) DeleteServerSnapshot(ctx context.Context, id int64) error {
	return a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		return a.store.DeleteServerSnapshot(ctx, tx, id)
	})
}

// CreateRestoreJob creates a new restore job record.
func (a *StoreAdapter) CreateRestoreJob(ctx context.Context) (*backup.RestoreJob, error) {
	var m models.RestoreJob
	err := a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		var err error
		m, err = a.store.CreateRestoreJob(ctx, tx)
		return db.WrapError(err)
	})
	if err != nil {
		return nil, fmt.Errorf("creating restore job: %w", err)
	}
	result := domainRestoreJob(m)
	return &result, nil
}

// GetRestoreJob returns a restore job by ID.
func (a *StoreAdapter) GetRestoreJob(ctx context.Context, id int64) (*backup.RestoreJob, error) {
	var m models.RestoreJob
	err := a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		var err error
		m, err = a.store.GetRestoreJob(ctx, tx, id)
		return db.WrapError(err)
	})
	if err != nil {
		return nil, backup.ErrRestoreJobNotFound
	}
	result := domainRestoreJob(m)
	return &result, nil
}

// ListRestoreJobsByStatus returns all restore jobs with the given status.
func (a *StoreAdapter) ListRestoreJobsByStatus(ctx context.Context, status backup.RestoreJobStatus) ([]backup.RestoreJob, error) {
	var models_ []models.RestoreJob
	err := a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		var err error
		models_, err = a.store.ListRestoreJobsByStatus(ctx, tx, string(status))
		return db.WrapError(err)
	})
	if err != nil {
		return nil, fmt.Errorf("listing restore jobs by status: %w", err)
	}
	result := make([]backup.RestoreJob, len(models_))
	for i, m := range models_ {
		result[i] = domainRestoreJob(m)
	}
	return result, nil
}

// UpdateRestoreJobStatus updates a restore job's status.
func (a *StoreAdapter) UpdateRestoreJobStatus(ctx context.Context, id int64, status backup.RestoreJobStatus) error {
	return a.db.TransactionContext(ctx, func(tx *db.Tx) error {
		return a.store.UpdateRestoreJobStatus(ctx, tx, id, string(status))
	})
}