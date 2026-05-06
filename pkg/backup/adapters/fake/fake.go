// Package fake provides in-memory fake implementations of the backup port interfaces.
// Per AGENTS.md: this is the reference implementation used by tests across packages
// to lock in the port's contract.
package fake

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/backup"
)

// FakeS3Provider is an in-memory implementation of backup.S3Provider.
type FakeS3Provider struct {
	mu           sync.RWMutex
	repoData     map[string][]byte     // key: "repoName/backupID"
	snapshotData map[int64][]byte      // key: snapshotID
	deleted      map[string]bool        // key: "repo/backupID" or "snapshot/ID" tracking deletes
	UploadErr    error                  // if set, UploadRepoBackup/UploadServerSnapshot returns this
	DownloadErr  error                  // if set, Download returns this
	DeleteErr    error                  // if set, Delete returns this
}

// NewFakeS3Provider creates a new FakeS3Provider.
func NewFakeS3Provider() *FakeS3Provider {
	return &FakeS3Provider{
		repoData:     make(map[string][]byte),
		snapshotData: make(map[int64][]byte),
		deleted:      make(map[string]bool),
	}
}

func repoKey(repoName string, backupID int64) string {
	return fmt.Sprintf("%s/%d", repoName, backupID)
}

// UploadRepoBackup stores repo backup data in memory.
func (f *FakeS3Provider) UploadRepoBackup(_ context.Context, repoName string, backupID int64, content []byte) error {
	if f.UploadErr != nil {
		return f.UploadErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repoData[repoKey(repoName, backupID)] = content
	return nil
}

// DownloadRepoBackup retrieves repo backup data from memory.
func (f *FakeS3Provider) DownloadRepoBackup(_ context.Context, repoName string, backupID int64) ([]byte, error) {
	if f.DownloadErr != nil {
		return nil, f.DownloadErr
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	key := repoKey(repoName, backupID)
	if f.deleted[key] {
		return nil, backup.ErrBackupNotFound
	}
	data, ok := f.repoData[key]
	if !ok {
		return nil, backup.ErrBackupNotFound
	}
	return data, nil
}

// DeleteRepoBackup removes repo backup data from memory.
func (f *FakeS3Provider) DeleteRepoBackup(_ context.Context, repoName string, backupID int64) error {
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := repoKey(repoName, backupID)
	delete(f.repoData, key)
	f.deleted[key] = true
	return nil
}

// UploadServerSnapshot stores snapshot data in memory.
func (f *FakeS3Provider) UploadServerSnapshot(_ context.Context, snapshotID int64, content []byte) error {
	if f.UploadErr != nil {
		return f.UploadErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshotData[snapshotID] = content
	return nil
}

// DownloadServerSnapshot retrieves snapshot data from memory.
func (f *FakeS3Provider) DownloadServerSnapshot(_ context.Context, snapshotID int64) ([]byte, error) {
	if f.DownloadErr != nil {
		return nil, f.DownloadErr
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.deleted[fmt.Sprintf("snapshot/%d", snapshotID)] {
		return nil, backup.ErrSnapshotNotFound
	}
	data, ok := f.snapshotData[snapshotID]
	if !ok {
		return nil, backup.ErrSnapshotNotFound
	}
	return data, nil
}

// DeleteServerSnapshot removes snapshot data from memory.
func (f *FakeS3Provider) DeleteServerSnapshot(_ context.Context, snapshotID int64) error {
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.snapshotData, snapshotID)
	f.deleted[fmt.Sprintf("snapshot/%d", snapshotID)] = true
	return nil
}

// FakeBundleProvider is an in-memory implementation of backup.BundleProvider.
type FakeBundleProvider struct {
	mu       sync.RWMutex
	bundles  map[string][]byte // key: repoName
	CreateFn func(ctx context.Context, repoName string) ([]byte, error)
	RestoreFn func(ctx context.Context, repoName string, content []byte) error
}

// NewFakeBundleProvider creates a new FakeBundleProvider.
func NewFakeBundleProvider() *FakeBundleProvider {
	return &FakeBundleProvider{
		bundles: make(map[string][]byte),
		CreateFn: func(ctx context.Context, repoName string) ([]byte, error) {
			return []byte(fmt.Sprintf("bundle-%s", repoName)), nil
		},
		RestoreFn: func(ctx context.Context, repoName string, content []byte) error {
			return nil
		},
	}
}

// CreateBundle creates a fake git bundle for the given repository.
func (f *FakeBundleProvider) CreateBundle(ctx context.Context, repoName string) ([]byte, error) {
	return f.CreateFn(ctx, repoName)
}

// RestoreFromBundle restores a repository from a fake git bundle.
func (f *FakeBundleProvider) RestoreFromBundle(ctx context.Context, repoName string, content []byte) error {
	return f.RestoreFn(ctx, repoName, content)
}

// FakeSnapshotDataProvider is an in-memory implementation of backup.SnapshotDataProvider.
type FakeSnapshotDataProvider struct {
	mu       sync.RWMutex
	snapshot []byte
	CreateFn func(ctx context.Context) ([]byte, error)
	RestoreFn func(ctx context.Context, content []byte) error
}

// NewFakeSnapshotDataProvider creates a new FakeSnapshotDataProvider.
func NewFakeSnapshotDataProvider() *FakeSnapshotDataProvider {
	return &FakeSnapshotDataProvider{
		CreateFn: func(ctx context.Context) ([]byte, error) {
			return []byte("server-snapshot-data"), nil
		},
		RestoreFn: func(ctx context.Context, content []byte) error {
			return nil
		},
	}
}

// CreateSnapshotData creates fake server snapshot data.
func (f *FakeSnapshotDataProvider) CreateSnapshotData(ctx context.Context) ([]byte, error) {
	return f.CreateFn(ctx)
}

// RestoreSnapshotData restores server data from a fake snapshot.
func (f *FakeSnapshotDataProvider) RestoreSnapshotData(ctx context.Context, content []byte) error {
	return f.RestoreFn(ctx, content)
}

// FakeBackupStore is an in-memory implementation of backup.BackupStore.
type FakeBackupStore struct {
	mu         sync.RWMutex
	schedule   *backup.BackupSchedule
	repoBackups map[int64]*backup.RepoBackup
	snapshots  map[int64]*backup.ServerSnapshot
	restoreJobs map[int64]*backup.RestoreJob
	nextID     int64

	// Error injection for testing error paths
	CreateRepoBackupErr     error
	UpdateRepoBackupErr     error
	CreateServerSnapshotErr error
	UpdateServerSnapshotErr error
	CreateRestoreJobErr     error
	UpdateRestoreJobErr     error
	GetBackupScheduleErr    error
	SetScheduleErr          error
	ListRepoBackupsErr      error
	ListSnapshotsErr        error
	ListRestoreJobsErr      error
	MarkRepoBackupsTimedOutErr error
	MarkSnapshotsTimedOutErr   error
	DeleteRepoBackupErr     error
	DeleteSnapshotErr       error

	// UpdateRestoreNonFailedErr returns an error from UpdateRestoreJobStatus
	// only when the target status is NOT "failed". This lets tests simulate
	// a failure in beginServerRestore (starting -> restoring_server) while
	// still allowing the cleanup path (starting -> failed).
	UpdateRestoreNonFailedErr error
}

// NewFakeBackupStore creates a new FakeBackupStore.
func NewFakeBackupStore() *FakeBackupStore {
	return &FakeBackupStore{
		repoBackups: make(map[int64]*backup.RepoBackup),
		snapshots:   make(map[int64]*backup.ServerSnapshot),
		restoreJobs: make(map[int64]*backup.RestoreJob),
		nextID:      1,
	}
}

func (f *FakeBackupStore) allocID() int64 {
	id := f.nextID
	f.nextID++
	return id
}

// GetBackupSchedule returns the stored backup schedule.
func (f *FakeBackupStore) GetBackupSchedule(_ context.Context) (*backup.BackupSchedule, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.GetBackupScheduleErr != nil {
		return nil, f.GetBackupScheduleErr
	}
	if f.schedule == nil {
		return nil, backup.ErrBackupNotFound
	}
	sched := *f.schedule
	return &sched, nil
}

// SetBackupScheduleNextRunAt sets or updates the backup schedule next run time.
func (f *FakeBackupStore) SetBackupScheduleNextRunAt(_ context.Context, nextRunAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SetScheduleErr != nil {
		return f.SetScheduleErr
	}
	if f.schedule == nil {
		f.schedule = &backup.BackupSchedule{NextRunAt: nextRunAt}
	} else {
		f.schedule.NextRunAt = nextRunAt
	}
	return nil
}

// CreateRepoBackup creates a new repo backup record.
func (f *FakeBackupStore) CreateRepoBackup(_ context.Context, repoName string, createdAt time.Time) (*backup.RepoBackup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateRepoBackupErr != nil {
		return nil, f.CreateRepoBackupErr
	}
	id := f.allocID()
	b := &backup.RepoBackup{
		ID:         id,
		RepoName:   repoName,
		CreatedAt:  createdAt,
		RetryCount: 0,
		Status:     backup.RepoBackupUploading,
	}
	f.repoBackups[id] = b
	result := *b
	return &result, nil
}

// GetRepoBackup returns a repo backup by ID.
func (f *FakeBackupStore) GetRepoBackup(_ context.Context, id int64) (*backup.RepoBackup, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.UpdateRepoBackupErr != nil {
		return nil, f.UpdateRepoBackupErr
	}
	b, ok := f.repoBackups[id]
	if !ok {
		return nil, backup.ErrBackupNotFound
	}
	result := *b
	return &result, nil
}

// ListRepoBackupsByRepo returns all repo backups for a given repo.
func (f *FakeBackupStore) ListRepoBackupsByRepo(_ context.Context, repoName string) ([]backup.RepoBackup, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.ListRepoBackupsErr != nil {
		return nil, f.ListRepoBackupsErr
	}
	var result []backup.RepoBackup
	for _, b := range f.repoBackups {
		if b.RepoName == repoName {
			result = append(result, *b)
		}
	}
	return result, nil
}

// ListRepoBackupsByStatus returns all repo backups with the given status.
func (f *FakeBackupStore) ListRepoBackupsByStatus(_ context.Context, status backup.RepoBackupStatus) ([]backup.RepoBackup, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var result []backup.RepoBackup
	for _, b := range f.repoBackups {
		if b.Status == status {
			result = append(result, *b)
		}
	}
	return result, nil
}

// UpdateRepoBackupStatus updates a repo backup's status and retry count.
func (f *FakeBackupStore) UpdateRepoBackupStatus(_ context.Context, id int64, status backup.RepoBackupStatus, retryCount int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.UpdateRepoBackupErr != nil {
		return f.UpdateRepoBackupErr
	}
	b, ok := f.repoBackups[id]
	if !ok {
		return backup.ErrBackupNotFound
	}
	b.Status = status
	b.RetryCount = retryCount
	return nil
}

// MarkRepoBackupsTimedOut marks all uploading repo backups older than cutoff as failed.
func (f *FakeBackupStore) MarkRepoBackupsTimedOut(_ context.Context, cutoff time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.MarkRepoBackupsTimedOutErr != nil {
		return 0, f.MarkRepoBackupsTimedOutErr
	}
	var count int64
	for _, b := range f.repoBackups {
		if b.Status == backup.RepoBackupUploading && !b.CreatedAt.After(cutoff) {
			b.Status = backup.RepoBackupFailed
			count++
		}
	}
	return count, nil
}

// DeleteRepoBackup deletes a repo backup record.
func (f *FakeBackupStore) DeleteRepoBackup(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.DeleteRepoBackupErr != nil {
		return f.DeleteRepoBackupErr
	}
	delete(f.repoBackups, id)
	return nil
}

// CreateServerSnapshot creates a new server snapshot record.
func (f *FakeBackupStore) CreateServerSnapshot(_ context.Context, createdAt time.Time) (*backup.ServerSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateServerSnapshotErr != nil {
		return nil, f.CreateServerSnapshotErr
	}
	id := f.allocID()
	s := &backup.ServerSnapshot{
		ID:         id,
		CreatedAt:  createdAt,
		RetryCount: 0,
		Status:     backup.ServerSnapshotUploading,
	}
	f.snapshots[id] = s
	result := *s
	return &result, nil
}

// GetServerSnapshot returns a server snapshot by ID.
func (f *FakeBackupStore) GetServerSnapshot(_ context.Context, id int64) (*backup.ServerSnapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	s, ok := f.snapshots[id]
	if !ok {
		return nil, backup.ErrSnapshotNotFound
	}
	result := *s
	return &result, nil
}

// ListServerSnapshotsByStatus returns all server snapshots with the given status.
func (f *FakeBackupStore) ListServerSnapshotsByStatus(_ context.Context, status backup.ServerSnapshotStatus) ([]backup.ServerSnapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.ListSnapshotsErr != nil {
		return nil, f.ListSnapshotsErr
	}
	var result []backup.ServerSnapshot
	for _, s := range f.snapshots {
		if s.Status == status {
			result = append(result, *s)
		}
	}
	return result, nil
}

// UpdateServerSnapshotStatus updates a server snapshot's status and retry count.
func (f *FakeBackupStore) UpdateServerSnapshotStatus(_ context.Context, id int64, status backup.ServerSnapshotStatus, retryCount int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.UpdateServerSnapshotErr != nil {
		return f.UpdateServerSnapshotErr
	}
	s, ok := f.snapshots[id]
	if !ok {
		return backup.ErrSnapshotNotFound
	}
	s.Status = status
	s.RetryCount = retryCount
	return nil
}

// MarkServerSnapshotsTimedOut marks all uploading server snapshots older than cutoff as failed.
func (f *FakeBackupStore) MarkServerSnapshotsTimedOut(_ context.Context, cutoff time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.MarkSnapshotsTimedOutErr != nil {
		return 0, f.MarkSnapshotsTimedOutErr
	}
	var count int64
	for _, s := range f.snapshots {
		if s.Status == backup.ServerSnapshotUploading && !s.CreatedAt.After(cutoff) {
			s.Status = backup.ServerSnapshotFailed
			count++
		}
	}
	return count, nil
}

// DeleteServerSnapshot deletes a server snapshot record.
func (f *FakeBackupStore) DeleteServerSnapshot(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.DeleteSnapshotErr != nil {
		return f.DeleteSnapshotErr
	}
	delete(f.snapshots, id)
	return nil
}

// CreateRestoreJob creates a new restore job record.
func (f *FakeBackupStore) CreateRestoreJob(_ context.Context) (*backup.RestoreJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateRestoreJobErr != nil {
		return nil, f.CreateRestoreJobErr
	}
	id := f.allocID()
	j := &backup.RestoreJob{
		ID:     id,
		Status: backup.RestoreJobStarting,
	}
	f.restoreJobs[id] = j
	result := *j
	return &result, nil
}

// GetRestoreJob returns a restore job by ID.
func (f *FakeBackupStore) GetRestoreJob(_ context.Context, id int64) (*backup.RestoreJob, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.UpdateRestoreJobErr != nil {
		return nil, f.UpdateRestoreJobErr
	}
	j, ok := f.restoreJobs[id]
	if !ok {
		return nil, backup.ErrRestoreJobNotFound
	}
	result := *j
	return &result, nil
}

// ListRestoreJobsByStatus returns all restore jobs with the given status.
func (f *FakeBackupStore) ListRestoreJobsByStatus(_ context.Context, status backup.RestoreJobStatus) ([]backup.RestoreJob, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.ListRestoreJobsErr != nil {
		return nil, f.ListRestoreJobsErr
	}
	var result []backup.RestoreJob
	for _, j := range f.restoreJobs {
		if j.Status == status {
			result = append(result, *j)
		}
	}
	return result, nil
}

// UpdateRestoreJobStatus updates a restore job's status.
func (f *FakeBackupStore) UpdateRestoreJobStatus(_ context.Context, id int64, status backup.RestoreJobStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if status != backup.RestoreJobFailed && f.UpdateRestoreNonFailedErr != nil {
		return f.UpdateRestoreNonFailedErr
	}
	if f.UpdateRestoreJobErr != nil {
		return f.UpdateRestoreJobErr
	}
	j, ok := f.restoreJobs[id]
	if !ok {
		return backup.ErrRestoreJobNotFound
	}
	j.Status = status
	return nil
}

// FakeRepoProvider is an in-memory implementation of backup.RepoProvider.
type FakeRepoProvider struct {
	mu    sync.RWMutex
	repos []backup.RepoInfo
}

// NewFakeRepoProvider creates a new FakeRepoProvider.
func NewFakeRepoProvider(repos []backup.RepoInfo) *FakeRepoProvider {
	return &FakeRepoProvider{repos: repos}
}

// ListRepos returns the configured repos.
func (f *FakeRepoProvider) ListRepos(_ context.Context) ([]backup.RepoInfo, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make([]backup.RepoInfo, len(f.repos))
	copy(result, f.repos)
	return result, nil
}