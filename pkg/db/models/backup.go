package models

import "time"

// BackupSchedule holds the next scheduled backup run time.
type BackupSchedule struct {
	ID        int64     `db:"id"`
	NextRunAt time.Time `db:"next_run_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// RepoBackup represents a single repository backup stored in S3.
type RepoBackup struct {
	ID         int64     `db:"id"`
	RepoName   string    `db:"repo_name"`
	CreatedAt  time.Time `db:"created_at"`
	RetryCount int       `db:"retry_count"`
	Status     string    `db:"status"`
}

// ServerSnapshot represents a full server snapshot stored in S3.
type ServerSnapshot struct {
	ID         int64     `db:"id"`
	CreatedAt  time.Time `db:"created_at"`
	RetryCount int       `db:"retry_count"`
	Status     string    `db:"status"`
}

// RestoreJob represents a full restore operation.
type RestoreJob struct {
	ID        int64     `db:"id"`
	Status    string    `db:"status"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}