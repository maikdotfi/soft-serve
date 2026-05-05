package database

import (
	"context"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/db/models"
	"github.com/charmbracelet/soft-serve/pkg/store"
)

type backupStore struct{}

var _ store.BackupStore = (*backupStore)(nil)

// --- BackupSchedule operations ---

func (*backupStore) GetBackupSchedule(ctx context.Context, h db.Handler) (models.BackupSchedule, error) {
	var schedule models.BackupSchedule
	query := h.Rebind("SELECT * FROM backup_schedule ORDER BY id LIMIT 1;")
	err := h.GetContext(ctx, &schedule, query)
	return schedule, db.WrapError(err)
}

func (*backupStore) SetBackupScheduleNextRunAt(ctx context.Context, h db.Handler, nextRunAt time.Time) error {
	// Upsert: try to update first, insert if no row exists.
	query := h.Rebind("UPDATE backup_schedule SET next_run_at = ?, updated_at = CURRENT_TIMESTAMP;")
	result, err := h.ExecContext(ctx, query, nextRunAt)
	if err != nil {
		return db.WrapError(err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		query = h.Rebind("INSERT INTO backup_schedule (next_run_at, updated_at) VALUES (?, CURRENT_TIMESTAMP);")
		_, err = h.ExecContext(ctx, query, nextRunAt)
		return db.WrapError(err)
	}
	return nil
}

// --- RepoBackup operations ---

func (*backupStore) CreateRepoBackup(ctx context.Context, h db.Handler, repoName string, createdAt time.Time) (models.RepoBackup, error) {
	var backup models.RepoBackup
	query := h.Rebind("INSERT INTO repo_backups (repo_name, created_at, retry_count, status) VALUES (?, ?, 0, 'uploading') RETURNING *;")
	err := h.GetContext(ctx, &backup, query, repoName, createdAt)
	if err != nil {
		// RETURNING may not be supported; fall back to INSERT then SELECT
		query = h.Rebind("INSERT INTO repo_backups (repo_name, created_at, retry_count, status) VALUES (?, ?, 0, 'uploading');")
		result, err := h.ExecContext(ctx, query, repoName, createdAt)
		if err != nil {
			return backup, db.WrapError(err)
		}
		id, _ := result.LastInsertId()
		return backup, db.WrapError(h.GetContext(ctx, &backup, h.Rebind("SELECT * FROM repo_backups WHERE id = ?;"), id))
	}
	return backup, nil
}

func (*backupStore) GetRepoBackup(ctx context.Context, h db.Handler, id int64) (models.RepoBackup, error) {
	var backup models.RepoBackup
	query := h.Rebind("SELECT * FROM repo_backups WHERE id = ?;")
	err := h.GetContext(ctx, &backup, query, id)
	return backup, db.WrapError(err)
}

func (*backupStore) ListRepoBackupsByRepo(ctx context.Context, h db.Handler, repoName string) ([]models.RepoBackup, error) {
	var backups []models.RepoBackup
	query := h.Rebind("SELECT * FROM repo_backups WHERE repo_name = ? ORDER BY created_at ASC;")
	err := h.SelectContext(ctx, &backups, query, repoName)
	return backups, db.WrapError(err)
}

func (*backupStore) ListRepoBackupsByStatus(ctx context.Context, h db.Handler, status string) ([]models.RepoBackup, error) {
	var backups []models.RepoBackup
	query := h.Rebind("SELECT * FROM repo_backups WHERE status = ? ORDER BY created_at ASC;")
	err := h.SelectContext(ctx, &backups, query, status)
	return backups, db.WrapError(err)
}

func (*backupStore) UpdateRepoBackupStatus(ctx context.Context, h db.Handler, id int64, status string, retryCount int) error {
	query := h.Rebind("UPDATE repo_backups SET status = ?, retry_count = ? WHERE id = ?;")
	_, err := h.ExecContext(ctx, query, status, retryCount, id)
	return db.WrapError(err)
}

func (*backupStore) MarkRepoBackupsTimedOut(ctx context.Context, h db.Handler, cutoff time.Time) (int64, error) {
	query := h.Rebind("UPDATE repo_backups SET status = 'failed' WHERE status = 'uploading' AND created_at <= ?;")
	result, err := h.ExecContext(ctx, query, cutoff)
	if err != nil {
		return 0, db.WrapError(err)
	}
	affected, _ := result.RowsAffected()
	return affected, nil
}

func (*backupStore) DeleteRepoBackup(ctx context.Context, h db.Handler, id int64) error {
	query := h.Rebind("DELETE FROM repo_backups WHERE id = ?;")
	_, err := h.ExecContext(ctx, query, id)
	return db.WrapError(err)
}

// --- ServerSnapshot operations ---

func (*backupStore) CreateServerSnapshot(ctx context.Context, h db.Handler, createdAt time.Time) (models.ServerSnapshot, error) {
	var snapshot models.ServerSnapshot
	query := h.Rebind("INSERT INTO server_snapshots (created_at, retry_count, status) VALUES (?, 0, 'uploading') RETURNING *;")
	err := h.GetContext(ctx, &snapshot, query, createdAt)
	if err != nil {
		query = h.Rebind("INSERT INTO server_snapshots (created_at, retry_count, status) VALUES (?, 0, 'uploading');")
		result, err := h.ExecContext(ctx, query, createdAt)
		if err != nil {
			return snapshot, db.WrapError(err)
		}
		id, _ := result.LastInsertId()
		return snapshot, db.WrapError(h.GetContext(ctx, &snapshot, h.Rebind("SELECT * FROM server_snapshots WHERE id = ?;"), id))
	}
	return snapshot, nil
}

func (*backupStore) GetServerSnapshot(ctx context.Context, h db.Handler, id int64) (models.ServerSnapshot, error) {
	var snapshot models.ServerSnapshot
	query := h.Rebind("SELECT * FROM server_snapshots WHERE id = ?;")
	err := h.GetContext(ctx, &snapshot, query, id)
	return snapshot, db.WrapError(err)
}

func (*backupStore) ListServerSnapshotsByStatus(ctx context.Context, h db.Handler, status string) ([]models.ServerSnapshot, error) {
	var snapshots []models.ServerSnapshot
	query := h.Rebind("SELECT * FROM server_snapshots WHERE status = ? ORDER BY created_at ASC;")
	err := h.SelectContext(ctx, &snapshots, query, status)
	return snapshots, db.WrapError(err)
}

func (*backupStore) UpdateServerSnapshotStatus(ctx context.Context, h db.Handler, id int64, status string, retryCount int) error {
	query := h.Rebind("UPDATE server_snapshots SET status = ?, retry_count = ? WHERE id = ?;")
	_, err := h.ExecContext(ctx, query, status, retryCount, id)
	return db.WrapError(err)
}

func (*backupStore) MarkServerSnapshotsTimedOut(ctx context.Context, h db.Handler, cutoff time.Time) (int64, error) {
	query := h.Rebind("UPDATE server_snapshots SET status = 'failed' WHERE status = 'uploading' AND created_at <= ?;")
	result, err := h.ExecContext(ctx, query, cutoff)
	if err != nil {
		return 0, db.WrapError(err)
	}
	affected, _ := result.RowsAffected()
	return affected, nil
}

func (*backupStore) DeleteServerSnapshot(ctx context.Context, h db.Handler, id int64) error {
	query := h.Rebind("DELETE FROM server_snapshots WHERE id = ?;")
	_, err := h.ExecContext(ctx, query, id)
	return db.WrapError(err)
}

// --- RestoreJob operations ---

func (*backupStore) CreateRestoreJob(ctx context.Context, h db.Handler) (models.RestoreJob, error) {
	var job models.RestoreJob
	query := h.Rebind("INSERT INTO restore_jobs (status) VALUES ('starting') RETURNING *;")
	err := h.GetContext(ctx, &job, query)
	if err != nil {
		query = h.Rebind("INSERT INTO restore_jobs (status) VALUES ('starting');")
		result, err := h.ExecContext(ctx, query)
		if err != nil {
			return job, db.WrapError(err)
		}
		id, _ := result.LastInsertId()
		return job, db.WrapError(h.GetContext(ctx, &job, h.Rebind("SELECT * FROM restore_jobs WHERE id = ?;"), id))
	}
	return job, nil
}

func (*backupStore) GetRestoreJob(ctx context.Context, h db.Handler, id int64) (models.RestoreJob, error) {
	var job models.RestoreJob
	query := h.Rebind("SELECT * FROM restore_jobs WHERE id = ?;")
	err := h.GetContext(ctx, &job, query, id)
	return job, db.WrapError(err)
}

func (*backupStore) ListRestoreJobsByStatus(ctx context.Context, h db.Handler, status string) ([]models.RestoreJob, error) {
	var jobs []models.RestoreJob
	query := h.Rebind("SELECT * FROM restore_jobs WHERE status = ? ORDER BY created_at ASC;")
	err := h.SelectContext(ctx, &jobs, query, status)
	return jobs, db.WrapError(err)
}

func (*backupStore) UpdateRestoreJobStatus(ctx context.Context, h db.Handler, id int64, status string) error {
	query := h.Rebind("UPDATE restore_jobs SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;")
	_, err := h.ExecContext(ctx, query, status, id)
	return db.WrapError(err)
}