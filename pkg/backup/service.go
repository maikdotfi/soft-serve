// Package backup implements the S3 backup and restore domain for Soft Serve.
// This file contains the BackupService which orchestrates all backup/restore
// operations by coordinating the port interfaces (BackupStore, S3Provider,
// BundleProvider, SnapshotDataProvider, Clock, RepoProvider).
package backup

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"charm.land/log/v2"
)

// BackupService orchestrates all backup and restore operations.
// It depends only on port interfaces, never on adapter packages.
type BackupService struct {
	cfg       BackupConfig
	store     BackupStore
	s3        S3Provider
	bundler   BundleProvider
	snapshot  SnapshotDataProvider
	repos     RepoProvider
	clock     Clock
	logger    *log.Logger
}

// NewBackupService creates a new BackupService.
func NewBackupService(
	cfg BackupConfig,
	store BackupStore,
	s3 S3Provider,
	bundler BundleProvider,
	snapshot SnapshotDataProvider,
	repos RepoProvider,
	clock Clock,
	logger *log.Logger,
) *BackupService {
	if logger == nil {
		logger = log.Default()
	}
	return &BackupService{
		cfg:      cfg,
		store:    store,
		s3:       s3,
		bundler:  bundler,
		snapshot: snapshot,
		repos:    repos,
		clock:    clock,
		logger:   logger.WithPrefix("backup"),
	}
}

// --- Schedule operations ---

// CreateDefaultBackupSchedule creates the default BackupSchedule if one
// doesn't already exist. Per spec: default BackupSchedule main = { next_run_at: now + config.schedule_interval }
func (s *BackupService) CreateDefaultBackupSchedule(ctx context.Context) error {
	existing, err := s.store.GetBackupSchedule(ctx)
	if err != nil && !errors.Is(err, ErrBackupNotFound) {
		return fmt.Errorf("checking backup schedule: %w", err)
	}
	if existing != nil {
		return nil // already exists
	}
	nextRunAt := s.clock.Now().Add(s.cfg.ScheduleInterval)
	if err := s.store.SetBackupScheduleNextRunAt(ctx, nextRunAt); err != nil {
		return fmt.Errorf("creating default backup schedule: %w", err)
	}
	return nil
}

// --- Repo backup: push-triggered (rule CreateRepoBackupOnPush) ---

// HandlePushToDefaultBranch is called when a push to the default branch is
// detected. Per spec rule CreateRepoBackupOnPush: creates a RepoBackup with
// status=uploading, then starts the upload process asynchronously.
func (s *BackupService) HandlePushToDefaultBranch(ctx context.Context, repoName string) error {
	s.logger.Info("push to default branch detected, creating repo backup", "repo", repoName)
	backup, err := s.store.CreateRepoBackup(ctx, repoName, s.clock.Now())
	if err != nil {
		return fmt.Errorf("creating repo backup for %s: %w", repoName, err)
	}
	// Start the upload asynchronously
	go s.uploadRepoBackup(context.Background(), backup)
	return nil
}

// --- Scheduled operations (rule FireBackupSchedule) ---

// Tick checks if the backup schedule should fire and runs the appropriate
// operations. Per spec rules FireBackupSchedule, CreateServerSnapshot, and
// CreateScheduledRepoBackups.
func (s *BackupService) Tick(ctx context.Context) error {
	schedule, err := s.store.GetBackupSchedule(ctx)
	if err != nil {
		return fmt.Errorf("getting backup schedule: %w", err)
	}
	if schedule == nil {
		// No schedule yet, create default
		if err := s.CreateDefaultBackupSchedule(ctx); err != nil {
			return err
		}
		schedule, err = s.store.GetBackupSchedule(ctx)
		if err != nil {
			return fmt.Errorf("getting backup schedule: %w", err)
		}
	}

	now := s.clock.Now()
	if schedule.NextRunAt.After(now) {
		// Not yet time to fire
		return nil
	}

	// FireBackupSchedule: schedule has fired
	// Advance the schedule: next_run_at = now + config.schedule_interval
	newNextRunAt := now.Add(s.cfg.ScheduleInterval)
	if err := s.store.SetBackupScheduleNextRunAt(ctx, newNextRunAt); err != nil {
		return fmt.Errorf("advancing backup schedule: %w", err)
	}

	// CreateServerSnapshot rule
	snapshot, err := s.store.CreateServerSnapshot(ctx, now)
	if err != nil {
		s.logger.Error("failed to create server snapshot", "err", err)
	} else {
		go s.uploadServerSnapshot(context.Background(), snapshot)
	}

	// CreateScheduledRepoBackups rule (only if backup_repos_on_schedule = true)
	if s.cfg.BackupReposOnSchedule {
		repos, err := s.repos.ListRepos(ctx)
		if err != nil {
			s.logger.Error("failed to list repos for scheduled backup", "err", err)
		} else {
			for _, repo := range repos {
				backup, err := s.store.CreateRepoBackup(ctx, repo.Name, now)
				if err != nil {
					s.logger.Error("failed to create scheduled repo backup", "repo", repo.Name, "err", err)
					continue
				}
				go s.uploadRepoBackup(context.Background(), backup)
			}
		}
	}

	return nil
}

// --- Upload operations ---

// uploadRepoBackup performs the upload of a repo backup with exponential backoff retries.
// Per spec rules RepoBackupUploadSucceeds, RepoBackupUploadFails, RepoBackupUploadTimeout.
func (s *BackupService) uploadRepoBackup(ctx context.Context, backup *RepoBackup) {
	// Check if already timed out
	deadline := backup.CreatedAt.Add(s.cfg.UploadTimeout)
	if !deadline.After(s.clock.Now()) {
		// Already timed out before we started
		s.markRepoBackupFailed(ctx, backup.ID)
		return
	}

	for attempt := 0; attempt <= s.cfg.MaxUploadRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 2^attempt seconds, capped at 5 minutes
			backoff := time.Duration(math.Min(float64(time.Second*time.Duration(1<<uint(attempt))), float64(5*time.Minute)))
			s.logger.Info("retrying repo backup upload", "repo", backup.RepoName, "attempt", attempt, "backoff", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
		}

		// Check timeout again before attempting upload
		deadline := backup.CreatedAt.Add(s.cfg.UploadTimeout)
		if !deadline.After(s.clock.Now()) {
			s.markRepoBackupFailed(ctx, backup.ID)
			return
		}

		// Create the git bundle
		content, err := s.bundler.CreateBundle(ctx, backup.RepoName)
		if err != nil {
			s.logger.Error("failed to create git bundle", "repo", backup.RepoName, "err", err)
			s.handleRepoBackupUploadFailure(ctx, backup, attempt)
			continue
		}

		// Upload to S3
		err = s.s3.UploadRepoBackup(ctx, backup.RepoName, backup.ID, content)
		if err != nil {
			s.logger.Error("failed to upload repo backup to S3", "repo", backup.RepoName, "err", err)
			s.handleRepoBackupUploadFailure(ctx, backup, attempt)
			continue
		}

		// Success: RepoBackupUploadSucceeds rule
		if err := s.store.UpdateRepoBackupStatus(ctx, backup.ID, RepoBackupStored, backup.RetryCount); err != nil {
			s.logger.Error("failed to mark repo backup as stored", "id", backup.ID, "err", err)
			return
		}
		s.logger.Info("repo backup uploaded successfully", "repo", backup.RepoName, "id", backup.ID)

		// RotateRepoBackups rule: remove surplus stored backups
		s.rotateRepoBackups(ctx, backup.RepoName)
		return
	}

	// Exhausted all retries
	s.markRepoBackupFailed(ctx, backup.ID)
}

// handleRepoBackupUploadFailure implements the RepoBackupUploadFails rule:
// if retry_count < max_upload_retries: increment retry_count; else: status = failed
func (s *BackupService) handleRepoBackupUploadFailure(ctx context.Context, backup *RepoBackup, currentAttempt int) {
	current, err := s.store.GetRepoBackup(ctx, backup.ID)
	if err != nil {
		s.logger.Error("failed to get repo backup for retry handling", "id", backup.ID, "err", err)
		return
	}
	if current.Status != RepoBackupUploading {
		return // already transitioned (e.g. timed out)
	}

	if current.RetryCount < s.cfg.MaxUploadRetries {
		// Increment retry_count, stay in uploading
		newRetryCount := current.RetryCount + 1
		if err := s.store.UpdateRepoBackupStatus(ctx, current.ID, RepoBackupUploading, newRetryCount); err != nil {
			s.logger.Error("failed to update repo backup retry count", "id", current.ID, "err", err)
		}
		// Update local copy for next loop iteration
		backup.RetryCount = newRetryCount
	} else {
		// Exhausted retries: mark as failed
		s.markRepoBackupFailed(ctx, current.ID)
	}
}

func (s *BackupService) markRepoBackupFailed(ctx context.Context, id int64) {
	if err := s.store.UpdateRepoBackupStatus(ctx, id, RepoBackupFailed, 0); err != nil {
		s.logger.Error("failed to mark repo backup as failed", "id", id, "err", err)
	}
}

// uploadServerSnapshot performs the upload of a server snapshot with exponential backoff retries.
func (s *BackupService) uploadServerSnapshot(ctx context.Context, snapshot *ServerSnapshot) {
	deadline := snapshot.CreatedAt.Add(s.cfg.UploadTimeout)
	if !deadline.After(s.clock.Now()) {
		s.markServerSnapshotFailed(ctx, snapshot.ID)
		return
	}

	for attempt := 0; attempt <= s.cfg.MaxUploadRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Min(float64(time.Second*time.Duration(1<<uint(attempt))), float64(5*time.Minute)))
			s.logger.Info("retrying server snapshot upload", "id", snapshot.ID, "attempt", attempt, "backoff", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
		}

		deadline := snapshot.CreatedAt.Add(s.cfg.UploadTimeout)
		if !deadline.After(s.clock.Now()) {
			s.markServerSnapshotFailed(ctx, snapshot.ID)
			return
		}

		// Create the snapshot data
		content, err := s.snapshot.CreateSnapshotData(ctx)
		if err != nil {
			s.logger.Error("failed to create snapshot data", "err", err)
			s.handleServerSnapshotUploadFailure(ctx, snapshot, attempt)
			continue
		}

		// Upload to S3
		err = s.s3.UploadServerSnapshot(ctx, snapshot.ID, content)
		if err != nil {
			s.logger.Error("failed to upload server snapshot to S3", "id", snapshot.ID, "err", err)
			s.handleServerSnapshotUploadFailure(ctx, snapshot, attempt)
			continue
		}

		// Success: ServerSnapshotUploadSucceeds rule
		if err := s.store.UpdateServerSnapshotStatus(ctx, snapshot.ID, ServerSnapshotStored, snapshot.RetryCount); err != nil {
			s.logger.Error("failed to mark server snapshot as stored", "id", snapshot.ID, "err", err)
			return
		}
		s.logger.Info("server snapshot uploaded successfully", "id", snapshot.ID)

		// RotateServerSnapshots rule: remove surplus stored snapshots
		s.rotateServerSnapshots(ctx)
		return
	}

	s.markServerSnapshotFailed(ctx, snapshot.ID)
}

func (s *BackupService) handleServerSnapshotUploadFailure(ctx context.Context, snapshot *ServerSnapshot, currentAttempt int) {
	current, err := s.store.GetServerSnapshot(ctx, snapshot.ID)
	if err != nil {
		s.logger.Error("failed to get server snapshot for retry handling", "id", snapshot.ID, "err", err)
		return
	}
	if current.Status != ServerSnapshotUploading {
		return
	}

	if current.RetryCount < s.cfg.MaxUploadRetries {
		newRetryCount := current.RetryCount + 1
		if err := s.store.UpdateServerSnapshotStatus(ctx, current.ID, ServerSnapshotUploading, newRetryCount); err != nil {
			s.logger.Error("failed to update server snapshot retry count", "id", current.ID, "err", err)
		}
		snapshot.RetryCount = newRetryCount
	} else {
		s.markServerSnapshotFailed(ctx, current.ID)
	}
}

func (s *BackupService) markServerSnapshotFailed(ctx context.Context, id int64) {
	if err := s.store.UpdateServerSnapshotStatus(ctx, id, ServerSnapshotFailed, 0); err != nil {
		s.logger.Error("failed to mark server snapshot as failed", "id", id, "err", err)
	}
}

// --- Rotation (rules RotateRepoBackups, RotateServerSnapshots) ---

// rotateRepoBackups removes the oldest stored backups for a repo that exceed
// max_repo_backups. Per spec: BackupsToRotate returns stored RepoBackups for
// the same repo where total stored count exceeds max_repo_backups, ordered by
// created_at ascending (oldest first).
func (s *BackupService) rotateRepoBackups(ctx context.Context, repoName string) {
	backups, err := s.store.ListRepoBackupsByRepo(ctx, repoName)
	if err != nil {
		s.logger.Error("failed to list repo backups for rotation", "repo", repoName, "err", err)
		return
	}

	// Filter to stored backups only and count
	var storedBackups []RepoBackup
	for _, b := range backups {
		if b.Status == RepoBackupStored {
			storedBackups = append(storedBackups, b)
		}
	}

	// Already within limits
	if len(storedBackups) <= s.cfg.MaxRepoBackups {
		return
	}

	// Sort by CreatedAt ascending (oldest first) — they should come from DB
	// already sorted, but let's be safe
	sortedBackups := sortedByCreatedAt(storedBackups)

	// Remove the surplus: oldest backups first
	surplus := len(storedBackups) - s.cfg.MaxRepoBackups
	for i := 0; i < surplus; i++ {
		b := sortedBackups[i]
		// Delete from S3 (best effort — per spec, delete from S3 even if repo deleted)
		if err := s.s3.DeleteRepoBackup(ctx, b.RepoName, b.ID); err != nil {
			s.logger.Error("failed to delete repo backup from S3", "repo", b.RepoName, "id", b.ID, "err", err)
			// Continue regardless — we still want to clean up the DB record
		}
		// Delete from store
		if err := s.store.DeleteRepoBackup(ctx, b.ID); err != nil {
			s.logger.Error("failed to delete repo backup from store", "id", b.ID, "err", err)
		}
	}
}

// rotateServerSnapshots removes the oldest stored server snapshots that exceed
// max_server_snapshots. Same logic as RepoBackups rotation.
func (s *BackupService) rotateServerSnapshots(ctx context.Context) {
	snapshots, err := s.store.ListServerSnapshotsByStatus(ctx, ServerSnapshotStored)
	if err != nil {
		s.logger.Error("failed to list server snapshots for rotation", "err", err)
		return
	}

	if len(snapshots) <= s.cfg.MaxServerSnapshots {
		return
	}

	// Sort by CreatedAt ascending
	sortedSnapshots := sortedSnapshotsByCreatedAt(snapshots)

	surplus := len(snapshots) - s.cfg.MaxServerSnapshots
	for i := 0; i < surplus; i++ {
		snap := sortedSnapshots[i]
		if err := s.s3.DeleteServerSnapshot(ctx, snap.ID); err != nil {
			s.logger.Error("failed to delete server snapshot from S3", "id", snap.ID, "err", err)
		}
		if err := s.store.DeleteServerSnapshot(ctx, snap.ID); err != nil {
			s.logger.Error("failed to delete server snapshot from store", "id", snap.ID, "err", err)
		}
	}
}

// --- Timeout enforcement (rules RepoBackupUploadTimeout, ServerSnapshotUploadTimeout) ---

// EnforceTimeouts marks backups and snapshots that have exceeded the upload
// timeout as failed. Should be called periodically.
func (s *BackupService) EnforceTimeouts(ctx context.Context) error {
	now := s.clock.Now()
	cutoff := now.Add(-s.cfg.UploadTimeout)

	affected, err := s.store.MarkRepoBackupsTimedOut(ctx, cutoff)
	if err != nil {
		s.logger.Error("failed to enforce repo backup timeouts", "err", err)
	} else if affected > 0 {
		s.logger.Info("marked timed out repo backups as failed", "count", affected)
	}

	affected, err = s.store.MarkServerSnapshotsTimedOut(ctx, cutoff)
	if err != nil {
		s.logger.Error("failed to enforce server snapshot timeouts", "err", err)
	} else if affected > 0 {
		s.logger.Info("marked timed out server snapshots as failed", "count", affected)
	}

	return nil
}

// --- Restore operations ---

// StartRestore initiates a full restore. Per spec rule StartRestore:
// Admin triggers a full restore; a RestoreJob is created with status=starting.
// Per spec: requires that the user is an admin.
func (s *BackupService) StartRestore(ctx context.Context, admin UserInfo) (*RestoreJob, error) {
	if !admin.IsAdmin() {
		return nil, ErrNotAdmin
	}

	job, err := s.store.CreateRestoreJob(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating restore job: %w", err)
	}

	// BeginServerRestore: auto-transition starting -> restoring_server
	if err := s.beginServerRestore(ctx, job); err != nil {
		// Transition to failed rather than leaving the job orphaned in starting.
		// Per spec rule RestoreFailedFromStarting: starting -> failed.
		s.failRestoreJob(ctx, job.ID)
		return nil, err
	}

	return job, nil
}

// beginServerRestore transitions the job to restoring_server and begins restoring
// server data. Per spec rule BeginServerRestore: auto-transition from starting.
func (s *BackupService) beginServerRestore(ctx context.Context, job *RestoreJob) error {
	// Transition: starting -> restoring_server
	if !job.CanTransition(RestoreJobRestoringServer) {
		return fmt.Errorf("%w: cannot transition from %s to restoring_server", ErrInvalidTransition, job.Status)
	}

	if err := s.store.UpdateRestoreJobStatus(ctx, job.ID, RestoreJobRestoringServer); err != nil {
		return fmt.Errorf("updating restore job to restoring_server: %w", err)
	}

	// Start restore in background
	go s.restoreServer(context.Background(), job.ID)
	return nil
}

// restoreServer restores server data from the most recent stored snapshot.
// Per spec rule ServerDataRestored: transitions to restoring_repos on success.
func (s *BackupService) restoreServer(ctx context.Context, jobID int64) {
	// Get the most recent stored server snapshot
	snapshots, err := s.store.ListServerSnapshotsByStatus(ctx, ServerSnapshotStored)
	if err != nil {
		s.logger.Error("failed to list server snapshots for restore", "err", err)
		s.failRestoreJob(ctx, jobID)
		return
	}
	if len(snapshots) == 0 {
		s.logger.Error("no stored server snapshots found for restore")
		s.failRestoreJob(ctx, jobID)
		return
	}

	// Find the most recent snapshot
	snapshot := snapshots[len(snapshots)-1]
	for _, snap := range snapshots {
		if snap.CreatedAt.After(snapshot.CreatedAt) {
			snapshot = snap
		}
	}

	// Download snapshot from S3
	content, err := s.s3.DownloadServerSnapshot(ctx, snapshot.ID)
	if err != nil {
		s.logger.Error("failed to download server snapshot for restore", "id", snapshot.ID, "err", err)
		s.failRestoreJob(ctx, jobID)
		return
	}

	// Restore server data
	if err := s.snapshot.RestoreSnapshotData(ctx, content); err != nil {
		s.logger.Error("failed to restore server data", "err", err)
		s.failRestoreJob(ctx, jobID)
		return
	}

	// Transition: restoring_server -> restoring_repos
	if err := s.store.UpdateRestoreJobStatus(ctx, jobID, RestoreJobRestoringRepos); err != nil {
		s.logger.Error("failed to update restore job to restoring_repos", "id", jobID, "err", err)
		s.failRestoreJob(ctx, jobID)
		return
	}

	// Continue to restore repos
	s.restoreRepos(ctx, jobID)
}

// restoreRepos restores all repositories from their most recent stored backup.
// Per spec rule AllReposRestored: transitions to completed on success.
// Per spec guidance: each repo is restored one at a time; each restore is
// atomic. On failure, the process can be re-attempted from the last
// successfully restored repo. The implementation checks local state to
// determine what has already been restored.
func (s *BackupService) restoreRepos(ctx context.Context, jobID int64) {
	// Get all stored repo backups, grouped by repo
	repos, err := s.repos.ListRepos(ctx)
	if err != nil {
		s.logger.Error("failed to list repos for restore", "err", err)
		s.failRestoreJob(ctx, jobID)
		return
	}

	for _, repo := range repos {
		// Per spec guidance: restore is idempotent. Check if this repo
		// already exists locally (already restored).
		backups, err := s.store.ListRepoBackupsByRepo(ctx, repo.Name)
		if err != nil {
			s.logger.Error("failed to list repo backups for restore", "repo", repo.Name, "err", err)
			s.failRestoreJob(ctx, jobID)
			return
		}

		// Find the most recent stored backup for this repo
		var latestBackup *RepoBackup
		for i := range backups {
			b := backups[i]
			if b.Status == RepoBackupStored {
				if latestBackup == nil || b.CreatedAt.After(latestBackup.CreatedAt) {
					latestBackup = &b
				}
			}
		}

		if latestBackup == nil {
			s.logger.Warn("no stored backup found for repo, skipping", "repo", repo.Name)
			continue
		}

		// Download bundle from S3
		content, err := s.s3.DownloadRepoBackup(ctx, repo.Name, latestBackup.ID)
		if err != nil {
			s.logger.Error("failed to download repo backup for restore", "repo", repo.Name, "err", err)
			s.failRestoreJob(ctx, jobID)
			return
		}

		// Restore repo from bundle
		if err := s.bundler.RestoreFromBundle(ctx, repo.Name, content); err != nil {
			s.logger.Error("failed to restore repo from bundle", "repo", repo.Name, "err", err)
			s.failRestoreJob(ctx, jobID)
			return
		}

		s.logger.Info("restored repo from backup", "repo", repo.Name, "backup_id", latestBackup.ID)
	}

	// All repos restored: transition to completed
	if err := s.store.UpdateRestoreJobStatus(ctx, jobID, RestoreJobCompleted); err != nil {
		s.logger.Error("failed to update restore job to completed", "id", jobID, "err", err)
		s.failRestoreJob(ctx, jobID)
		return
	}

	s.logger.Info("restore job completed", "id", jobID)
}

func (s *BackupService) failRestoreJob(ctx context.Context, jobID int64) {
	if err := s.store.UpdateRestoreJobStatus(ctx, jobID, RestoreJobFailed); err != nil {
		s.logger.Error("failed to mark restore job as failed", "id", jobID, "err", err)
	}
}

// --- Manual trigger ---

// TriggerServerSnapshot manually creates a server snapshot (for admin use).
func (s *BackupService) TriggerServerSnapshot(ctx context.Context) (*ServerSnapshot, error) {
	snapshot, err := s.store.CreateServerSnapshot(ctx, s.clock.Now())
	if err != nil {
		return nil, fmt.Errorf("creating server snapshot: %w", err)
	}
	go s.uploadServerSnapshot(context.Background(), snapshot)
	return snapshot, nil
}

// TriggerRepoBackup manually creates a repo backup (for admin use).
func (s *BackupService) TriggerRepoBackup(ctx context.Context, repoName string) (*RepoBackup, error) {
	backup, err := s.store.CreateRepoBackup(ctx, repoName, s.clock.Now())
	if err != nil {
		return nil, fmt.Errorf("creating repo backup for %s: %w", repoName, err)
	}
	go s.uploadRepoBackup(context.Background(), backup)
	return backup, nil
}

// --- Query operations for AdminBackupManagement surface ---

// ListStoredRepoBackups returns all stored repo backups (exposed by AdminBackupManagement surface).
func (s *BackupService) ListStoredRepoBackups(ctx context.Context, repoName string) ([]RepoBackup, error) {
	backups, err := s.store.ListRepoBackupsByRepo(ctx, repoName)
	if err != nil {
		return nil, fmt.Errorf("listing repo backups: %w", err)
	}
	var stored []RepoBackup
	for _, b := range backups {
		if b.Status == RepoBackupStored {
			stored = append(stored, b)
		}
	}
	return stored, nil
}

// ListStoredServerSnapshots returns all stored server snapshots (exposed by AdminBackupManagement surface).
func (s *BackupService) ListStoredServerSnapshots(ctx context.Context) ([]ServerSnapshot, error) {
	return s.store.ListServerSnapshotsByStatus(ctx, ServerSnapshotStored)
}

// ListActiveRestoreJobs returns all non-completed restore jobs (exposed by AdminBackupManagement surface).
func (s *BackupService) ListActiveRestoreJobs(ctx context.Context) ([]RestoreJob, error) {
	var jobs []RestoreJob
	for _, status := range []RestoreJobStatus{
		RestoreJobStarting,
		RestoreJobRestoringServer,
		RestoreJobRestoringRepos,
		RestoreJobFailed,
	} {
		statusJobs, err := s.store.ListRestoreJobsByStatus(ctx, status)
		if err != nil {
			return nil, fmt.Errorf("listing restore jobs: %w", err)
		}
		jobs = append(jobs, statusJobs...)
	}
	return jobs, nil
}

// IsConfigured returns true if the backup service has the minimum S3
// configuration needed to operate.
func (s *BackupService) IsConfigured() bool {
	return s.cfg.S3Endpoint != "" && s.cfg.S3Bucket != "" && s.cfg.S3Region != ""
}

// --- Sorting helpers ---

func sortedByCreatedAt(backups []RepoBackup) []RepoBackup {
	result := make([]RepoBackup, len(backups))
	copy(result, backups)
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i].CreatedAt.After(result[j].CreatedAt) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}

func sortedSnapshotsByCreatedAt(snapshots []ServerSnapshot) []ServerSnapshot {
	result := make([]ServerSnapshot, len(snapshots))
	copy(result, snapshots)
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i].CreatedAt.After(result[j].CreatedAt) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}