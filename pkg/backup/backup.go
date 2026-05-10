// Package backup implements the S3 backup and restore domain for Soft Serve.
// The spec is defined in backup.allium at the repository root.
//
// Architecture follows AGENTS.md: domain types and port interfaces live here;
// adapters (S3, database, filesystem) live in pkg/backup/adapters/.
// The domain package never imports adapter packages.
package backup

import (
	"context"
	"errors"
	"time"
)

// --- Config ---

// BackupConfig holds all configuration parameters for the backup subsystem.
// Corresponds to the config block in backup.allium.
type BackupConfig struct {
	S3Endpoint         string
	S3Bucket           string
	S3Region           string
	S3PathPrefix       string
	ScheduleInterval   time.Duration
	MaxRepoBackups     int
	MaxServerSnapshots int
	MaxUploadRetries   int
	UploadTimeout      time.Duration
}

// DefaultBackupConfig returns the config with defaults specified in the spec.
func DefaultBackupConfig() BackupConfig {
	return BackupConfig{
		S3PathPrefix:       "soft-serve",
		ScheduleInterval:   6 * time.Hour,
		MaxRepoBackups:     5,
		MaxServerSnapshots: 30,
		MaxUploadRetries:   3,
		UploadTimeout:      1 * time.Hour,
	}
}

// --- Status enums ---

// RepoBackupStatus represents the status of a RepoBackup.
// Spec: uploading | stored | failed
type RepoBackupStatus string

const (
	RepoBackupUploading RepoBackupStatus = "uploading"
	RepoBackupStored    RepoBackupStatus = "stored"
	RepoBackupFailed    RepoBackupStatus = "failed"
)

// ValidRepoBackupStatuses is the set of valid RepoBackupStatus values.
var ValidRepoBackupStatuses = map[RepoBackupStatus]bool{
	RepoBackupUploading: true,
	RepoBackupStored:    true,
	RepoBackupFailed:    true,
}

// ServerSnapshotStatus represents the status of a ServerSnapshot.
// Spec: uploading | stored | failed
type ServerSnapshotStatus string

const (
	ServerSnapshotUploading ServerSnapshotStatus = "uploading"
	ServerSnapshotStored    ServerSnapshotStatus = "stored"
	ServerSnapshotFailed    ServerSnapshotStatus = "failed"
)

// ValidServerSnapshotStatuses is the set of valid ServerSnapshotStatus values.
var ValidServerSnapshotStatuses = map[ServerSnapshotStatus]bool{
	ServerSnapshotUploading: true,
	ServerSnapshotStored:    true,
	ServerSnapshotFailed:    true,
}

// RestoreJobStatus represents the status of a RestoreJob.
// Spec: starting | restoring_server | restoring_repos | completed | failed
type RestoreJobStatus string

const (
	RestoreJobStarting         RestoreJobStatus = "starting"
	RestoreJobRestoringServer  RestoreJobStatus = "restoring_server"
	RestoreJobRestoringRepos   RestoreJobStatus = "restoring_repos"
	RestoreJobCompleted       RestoreJobStatus = "completed"
	RestoreJobFailed          RestoreJobStatus = "failed"
)

// ValidRestoreJobStatuses is the set of valid RestoreJobStatus values.
var ValidRestoreJobStatuses = map[RestoreJobStatus]bool{
	RestoreJobStarting:        true,
	RestoreJobRestoringServer: true,
	RestoreJobRestoringRepos:  true,
	RestoreJobCompleted:      true,
	RestoreJobFailed:         true,
}

// --- Entities ---

// BackupSchedule holds the next scheduled run time.
// Spec: entity BackupSchedule { next_run_at: Timestamp }
type BackupSchedule struct {
	NextRunAt time.Time
}

// RepoBackup represents a single repository backup.
// Spec: entity RepoBackup { repo: Repo, created_at: Timestamp, retry_count: Integer, status: uploading | stored | failed }
type RepoBackup struct {
	ID         int64
	RepoName   string
	CreatedAt  time.Time
	RetryCount int
	Status     RepoBackupStatus
}

// ServerSnapshot represents a full server snapshot.
// Spec: entity ServerSnapshot { created_at: Timestamp, retry_count: Integer, status: uploading | stored | failed }
type ServerSnapshot struct {
	ID         int64
	CreatedAt  time.Time
	RetryCount int
	Status     ServerSnapshotStatus
}

// RestoreJob represents a full restore operation.
// Spec: entity RestoreJob { status: starting | restoring_server | restoring_repos | completed | failed }
type RestoreJob struct {
	ID     int64
	Status RestoreJobStatus
}

// --- Transition graph definitions ---

// RepoBackupTransitions defines the valid transitions for RepoBackup.status.
// Spec: transitions status { uploading -> stored, uploading -> failed; terminal: stored, failed }
var RepoBackupTransitions = map[RepoBackupStatus][]RepoBackupStatus{
	RepoBackupUploading: {RepoBackupStored, RepoBackupFailed},
}

// RepoBackupTerminalStates defines terminal states with no outbound transitions.
var RepoBackupTerminalStates = map[RepoBackupStatus]bool{
	RepoBackupStored: true,
	RepoBackupFailed: true,
}

// ServerSnapshotTransitions defines the valid transitions for ServerSnapshot.status.
// Spec: transitions status { uploading -> stored, uploading -> failed; terminal: stored, failed }
var ServerSnapshotTransitions = map[ServerSnapshotStatus][]ServerSnapshotStatus{
	ServerSnapshotUploading: {ServerSnapshotStored, ServerSnapshotFailed},
}

// ServerSnapshotTerminalStates defines terminal states with no outbound transitions.
var ServerSnapshotTerminalStates = map[ServerSnapshotStatus]bool{
	ServerSnapshotStored: true,
	ServerSnapshotFailed: true,
}

// RestoreJobTransitions defines the valid transitions for RestoreJob.status.
// Spec:
//
//	transitions status {
//	  starting -> restoring_server
//	  restoring_server -> restoring_repos
//	  restoring_repos -> completed
//	  starting -> failed
//	  restoring_server -> failed
//	  restoring_repos -> failed
//	  terminal: completed, failed
//	}
var RestoreJobTransitions = map[RestoreJobStatus][]RestoreJobStatus{
	RestoreJobStarting:       {RestoreJobRestoringServer, RestoreJobFailed},
	RestoreJobRestoringServer: {RestoreJobRestoringRepos, RestoreJobFailed},
	RestoreJobRestoringRepos:  {RestoreJobCompleted, RestoreJobFailed},
}

// RestoreJobTerminalStates defines terminal states with no outbound transitions.
var RestoreJobTerminalStates = map[RestoreJobStatus]bool{
	RestoreJobCompleted: true,
	RestoreJobFailed:    true,
}

// CanTransition checks whether a transition from src to dst is valid for RepoBackup.
func (b *RepoBackup) CanTransition(dst RepoBackupStatus) bool {
	for _, s := range RepoBackupTransitions[b.Status] {
		if s == dst {
			return true
		}
	}
	return false
}

// CanTransition checks whether a transition from src to dst is valid for ServerSnapshot.
func (s *ServerSnapshot) CanTransition(dst ServerSnapshotStatus) bool {
	for _, st := range ServerSnapshotTransitions[s.Status] {
		if st == dst {
			return true
		}
	}
	return false
}

// CanTransition checks whether a transition from src to dst is valid for RestoreJob.
func (j *RestoreJob) CanTransition(dst RestoreJobStatus) bool {
	for _, s := range RestoreJobTransitions[j.Status] {
		if s == dst {
			return true
		}
	}
	return false
}

// IsTerminal returns whether the given RepoBackupStatus is terminal.
func (s RepoBackupStatus) IsTerminal() bool {
	return RepoBackupTerminalStates[s]
}

// IsTerminal returns whether the given ServerSnapshotStatus is terminal.
func (s ServerSnapshotStatus) IsTerminal() bool {
	return ServerSnapshotTerminalStates[s]
}

// IsTerminal returns whether the given RestoreJobStatus is terminal.
func (s RestoreJobStatus) IsTerminal() bool {
	return RestoreJobTerminalStates[s]
}

// --- Domain errors ---

// Sentinel errors for the backup domain. Adapters must translate to these.
var (
	ErrBackupNotFound      = errors.New("backup not found")
	ErrSnapshotNotFound   = errors.New("snapshot not found")
	ErrRestoreJobNotFound = errors.New("restore job not found")
	ErrInvalidTransition  = errors.New("invalid status transition")
	ErrUploadFailed       = errors.New("upload failed")
	ErrDownloadFailed     = errors.New("download failed")
	ErrNotAdmin           = errors.New("user is not an admin")
	ErrS3NotConfigured    = errors.New("S3 backup is not configured")
)

// --- Port interfaces ---
// Per AGENTS.md: all external dependencies go through ports defined in the domain.

// Clock provides time injection for temporal tests.
type Clock interface {
	Now() time.Time
}

// WallClock is the default Clock using wall-clock time.
type WallClock struct{}

func (WallClock) Now() time.Time { return time.Now() }

// RepoProvider gives access to repositories for backup operations.
// Maps to external entity Repo in the spec.
type RepoProvider interface {
	// ListRepos returns all repositories known to the server.
	ListRepos(ctx context.Context) ([]RepoInfo, error)
}

// RepoInfo represents a minimal view of a repository.
// Maps to external entity Repo { name, default_branch }.
type RepoInfo struct {
	Name          string
	DefaultBranch string
}

// UserInfo represents a user for actor identification.
// Maps to external entity User { role }.
type UserInfo struct {
	Role string
}

// IsAdmin checks whether the user has the admin role.
// Maps to actor Admin identified_by: User where role = admin.
func (u UserInfo) IsAdmin() bool {
	return u.Role == "admin"
}

// BackupStore is the port interface for persisting backup domain entities.
// Adapters (e.g. database) implement this interface.
type BackupStore interface {
	// BackupSchedule operations
	GetBackupSchedule(ctx context.Context) (*BackupSchedule, error)
	SetBackupScheduleNextRunAt(ctx context.Context, nextRunAt time.Time) error

	// RepoBackup operations
	CreateRepoBackup(ctx context.Context, repoName string, createdAt time.Time) (*RepoBackup, error)
	GetRepoBackup(ctx context.Context, id int64) (*RepoBackup, error)
	ListRepoBackupsByRepo(ctx context.Context, repoName string) ([]RepoBackup, error)
	ListRepoBackupsByStatus(ctx context.Context, status RepoBackupStatus) ([]RepoBackup, error)
	UpdateRepoBackupStatus(ctx context.Context, id int64, status RepoBackupStatus, retryCount int) error
	MarkRepoBackupsTimedOut(ctx context.Context, cutoff time.Time) (int64, error)
	DeleteRepoBackup(ctx context.Context, id int64) error

	// ServerSnapshot operations
	CreateServerSnapshot(ctx context.Context, createdAt time.Time) (*ServerSnapshot, error)
	GetServerSnapshot(ctx context.Context, id int64) (*ServerSnapshot, error)
	ListServerSnapshotsByStatus(ctx context.Context, status ServerSnapshotStatus) ([]ServerSnapshot, error)
	UpdateServerSnapshotStatus(ctx context.Context, id int64, status ServerSnapshotStatus, retryCount int) error
	MarkServerSnapshotsTimedOut(ctx context.Context, cutoff time.Time) (int64, error)
	DeleteServerSnapshot(ctx context.Context, id int64) error

	// RestoreJob operations
	CreateRestoreJob(ctx context.Context) (*RestoreJob, error)
	GetRestoreJob(ctx context.Context, id int64) (*RestoreJob, error)
	ListRestoreJobsByStatus(ctx context.Context, status RestoreJobStatus) ([]RestoreJob, error)
	UpdateRestoreJobStatus(ctx context.Context, id int64, status RestoreJobStatus) error
}

// S3Provider is the port interface for object storage operations.
// Maps to external entity S3Client in the spec.
// Adapters (e.g. AWS S3, MinIO, fake) implement this interface.
type S3Provider interface {
	// UploadRepoBackup uploads a git bundle for the given repo backup.
	// The bundle content is provided as a reader.
	UploadRepoBackup(ctx context.Context, repoName string, backupID int64, content []byte) error

	// DownloadRepoBackup downloads a git bundle for the given repo backup.
	// Returns the bundle content as bytes.
	DownloadRepoBackup(ctx context.Context, repoName string, backupID int64) ([]byte, error)

	// DeleteRepoBackup deletes the stored git bundle for the given repo backup.
	DeleteRepoBackup(ctx context.Context, repoName string, backupID int64) error

	// UploadServerSnapshot uploads a server snapshot archive.
	UploadServerSnapshot(ctx context.Context, snapshotID int64, content []byte) error

	// DownloadServerSnapshot downloads a server snapshot archive.
	DownloadServerSnapshot(ctx context.Context, snapshotID int64) ([]byte, error)

	// DeleteServerSnapshot deletes the stored server snapshot archive.
	DeleteServerSnapshot(ctx context.Context, snapshotID int64) error
}

// BundleProvider is the port interface for git bundle operations.
// This abstracts git bundle create/extract so the service stays testable.
type BundleProvider interface {
	// CreateBundle creates a git bundle for the given repository.
	// Returns the bundle content as bytes.
	CreateBundle(ctx context.Context, repoName string) ([]byte, error)

	// RestoreFromBundle restores a repository from a git bundle.
	RestoreFromBundle(ctx context.Context, repoName string, content []byte) error
}

// SnapshotDataProvider is the port interface for creating/restoring server data.
// This abstracts the database, config, and key storage for snapshots.
type SnapshotDataProvider interface {
	// CreateSnapshotData creates a snapshot of server data (DB, config, keys).
	// Returns the snapshot content as bytes.
	CreateSnapshotData(ctx context.Context) ([]byte, error)

	// RestoreSnapshotData restores server data from a snapshot archive.
	RestoreSnapshotData(ctx context.Context, content []byte) error
}