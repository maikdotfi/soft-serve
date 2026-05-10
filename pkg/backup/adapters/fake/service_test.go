package fake_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/backup"
	"github.com/charmbracelet/soft-serve/pkg/backup/adapters/fake"
	"github.com/matryer/is"
)

func newTestService(t *testing.T) (*backup.BackupService, *fake.FakeBackupStore, *fake.FakeS3Provider, *fake.FakeBundleProvider, *fakeClock) {
	t.Helper()
	cfg := backup.DefaultBackupConfig()
	cfg.S3Endpoint = "https://s3.example.com"
	cfg.S3Bucket = "test-bucket"
	cfg.S3Region = "us-east-1"

	store := fake.NewFakeBackupStore()
	s3 := fake.NewFakeS3Provider()
	bundler := fake.NewFakeBundleProvider()
	snapshot := fake.NewFakeSnapshotDataProvider()
	clock := newFakeClock()
	repos := fake.NewFakeRepoProvider([]backup.RepoInfo{
		{Name: "repo1", DefaultBranch: "main"},
		{Name: "repo2", DefaultBranch: "main"},
	})

	svc := backup.NewBackupService(cfg, store, s3, bundler, snapshot, repos, clock, nil)
	return svc, store, s3, bundler, clock
}

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }
func newFakeClock() *fakeClock               { return &fakeClock{now: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)} }

func TestService_Tick_CreatesServerSnapshot(t *testing.T) {
	is := is.New(t)
	svc, store, _, _, clock := newTestService(t)

	is.NoErr(store.SetBackupScheduleNextRunAt(context.Background(), clock.Now()))

	err := svc.Tick(context.Background())
	is.NoErr(err)

	uploading, err := store.ListServerSnapshotsByStatus(context.Background(), backup.ServerSnapshotUploading)
	is.NoErr(err)
	stored, err := store.ListServerSnapshotsByStatus(context.Background(), backup.ServerSnapshotStored)
	is.NoErr(err)
	is.Equal(len(uploading)+len(stored) >= 1, true)
}

// Tick must back up every repo on every schedule fire. Spec rule
// CreateScheduledRepoBackups is unconditional now that push-triggered
// backup is gone — there is no opt-out flag, so a fired schedule must
// touch every repo the RepoProvider knows about.
func TestService_Tick_AlwaysCreatesRepoBackups(t *testing.T) {
	is := is.New(t)
	svc, store, _, _, clock := newTestService(t)

	is.NoErr(store.SetBackupScheduleNextRunAt(context.Background(), clock.Now()))

	err := svc.Tick(context.Background())
	is.NoErr(err)

	backups1, _ := store.ListRepoBackupsByRepo(context.Background(), "repo1")
	backups2, _ := store.ListRepoBackupsByRepo(context.Background(), "repo2")
	is.Equal(len(backups1), 1)
	is.Equal(len(backups2), 1)
}

func TestService_LogScheduleReady_LogsNextScheduledRun(t *testing.T) {
	is := is.New(t)
	cfg := backup.DefaultBackupConfig()
	cfg.S3Endpoint = "https://s3.example.com"
	cfg.S3Bucket = "test-bucket"
	cfg.S3Region = "us-east-1"

	store := fake.NewFakeBackupStore()
	clock := newFakeClock()
	var buf bytes.Buffer
	logger := log.New(&buf)
	logger.SetFormatter(log.LogfmtFormatter)
	svc := backup.NewBackupService(
		cfg,
		store,
		fake.NewFakeS3Provider(),
		fake.NewFakeBundleProvider(),
		fake.NewFakeSnapshotDataProvider(),
		fake.NewFakeRepoProvider(nil),
		clock,
		logger,
	)

	err := svc.LogScheduleReady(context.Background())
	is.NoErr(err)

	out := buf.String()
	is.True(strings.Contains(out, "level=info"))
	is.True(strings.Contains(out, "msg=\"backup schedule ready\""))
	is.True(strings.Contains(out, "next_run_at="))
	is.True(strings.Contains(out, "run_in=6h0m0s"))
	is.True(strings.Contains(out, "schedule_interval=6h0m0s"))
}

func TestService_Tick_LogsScheduledRunSummary(t *testing.T) {
	is := is.New(t)
	cfg := backup.DefaultBackupConfig()
	cfg.S3Endpoint = "https://s3.example.com"
	cfg.S3Bucket = "test-bucket"
	cfg.S3Region = "us-east-1"
	cfg.UploadTimeout = 0

	store := fake.NewFakeBackupStore()
	clock := newFakeClock()
	var buf bytes.Buffer
	logger := log.New(&buf)
	logger.SetFormatter(log.LogfmtFormatter)
	svc := backup.NewBackupService(
		cfg,
		store,
		fake.NewFakeS3Provider(),
		fake.NewFakeBundleProvider(),
		fake.NewFakeSnapshotDataProvider(),
		fake.NewFakeRepoProvider([]backup.RepoInfo{
			{Name: "repo1", DefaultBranch: "main"},
			{Name: "repo2", DefaultBranch: "main"},
		}),
		clock,
		logger,
	)
	is.NoErr(store.SetBackupScheduleNextRunAt(context.Background(), clock.Now()))

	err := svc.Tick(context.Background())
	is.NoErr(err)

	out := buf.String()
	is.True(strings.Contains(out, "level=info"))
	is.True(strings.Contains(out, "msg=\"scheduled backup run summary\""))
	is.True(strings.Contains(out, "repo_count=2"))
	is.True(strings.Contains(out, "repo_backups_created=2"))
	is.True(strings.Contains(out, "repo_backup_create_failures=0"))
	is.True(strings.Contains(out, "server_snapshot_created=true"))
	is.True(strings.Contains(out, "next_run_at="))
}

func TestService_StartRestore_RequiresAdmin(t *testing.T) {
	is := is.New(t)
	svc, _, _, _, _ := newTestService(t)

	_, err := svc.StartRestore(context.Background(), backup.UserInfo{Role: "user"})
	is.Equal(err, backup.ErrNotAdmin)
}

func TestService_StartRestore_CreatesRestoreJob(t *testing.T) {
	is := is.New(t)
	svc, _, _, _, _ := newTestService(t)

	job, err := svc.StartRestore(context.Background(), backup.UserInfo{Role: "admin"})
	is.NoErr(err)
	is.Equal(job.Status, backup.RestoreJobStarting) // job starts in starting status

	// The job may transition asynchronously, so just verify it was created
	// and has a valid ID
	is.Equal(job.ID > 0, true) // job has an ID
}

func TestService_IsConfigured(t *testing.T) {
	is := is.New(t)
	svc, _, _, _, _ := newTestService(t)
	is.Equal(svc.IsConfigured(), true)

	cfg := backup.DefaultBackupConfig()
	svcNotConfigured := backup.NewBackupService(cfg, nil, nil, nil, nil, nil, newFakeClock(), nil)
	is.Equal(svcNotConfigured.IsConfigured(), false)
}

func TestService_TriggerServerSnapshot(t *testing.T) {
	is := is.New(t)
	svc, _, _, _, _ := newTestService(t)

	snapshot, err := svc.TriggerServerSnapshot(context.Background())
	is.NoErr(err)
	is.Equal(snapshot.Status, backup.ServerSnapshotUploading)
}

func TestService_TriggerRepoBackup(t *testing.T) {
	is := is.New(t)
	svc, _, _, _, _ := newTestService(t)

	b, err := svc.TriggerRepoBackup(context.Background(), "repo1")
	is.NoErr(err)
	is.Equal(b.RepoName, "repo1")
	is.Equal(b.Status, backup.RepoBackupUploading)
}

func TestService_EnforceTimeouts(t *testing.T) {
	is := is.New(t)
	svc, store, _, _, clock := newTestService(t)
	cfg := backup.DefaultBackupConfig()

	// Create a backup that's been uploading for over the timeout duration
	_, err := store.CreateRepoBackup(context.Background(), "repo1", clock.Now())
	is.NoErr(err)

	// Create a snapshot that's been uploading for over the timeout duration
	_, err = store.CreateServerSnapshot(context.Background(), clock.Now())
	is.NoErr(err)

	// Advance time past the upload timeout
	clock.Advance(cfg.UploadTimeout + 1*time.Second)

	err = svc.EnforceTimeouts(context.Background())
	is.NoErr(err)

	// Check that the backup was marked as failed
	backups, _ := store.ListRepoBackupsByRepo(context.Background(), "repo1")
	is.Equal(len(backups), 1)
	is.Equal(backups[0].Status, backup.RepoBackupFailed)

	// Check that the snapshot was marked as failed
	snapshots, _ := store.ListServerSnapshotsByStatus(context.Background(), backup.ServerSnapshotFailed)
	is.Equal(len(snapshots), 1)
}

func TestService_ListStoredRepoBackups(t *testing.T) {
	is := is.New(t)
	svc, store, _, _, _ := newTestService(t)

	b, err := store.CreateRepoBackup(context.Background(), "repo1", time.Now())
	is.NoErr(err)
	is.NoErr(store.UpdateRepoBackupStatus(context.Background(), b.ID, backup.RepoBackupStored, 0))

	// Create an uploading backup (shouldn't be listed)
	_, err = store.CreateRepoBackup(context.Background(), "repo1", time.Now())
	is.NoErr(err)

	backups, err := svc.ListStoredRepoBackups(context.Background(), "repo1")
	is.NoErr(err)
	is.Equal(len(backups), 1)
	is.Equal(backups[0].Status, backup.RepoBackupStored)
}

func TestService_ListStoredServerSnapshots(t *testing.T) {
	is := is.New(t)
	svc, store, _, _, _ := newTestService(t)

	s, err := store.CreateServerSnapshot(context.Background(), time.Now())
	is.NoErr(err)
	is.NoErr(store.UpdateServerSnapshotStatus(context.Background(), s.ID, backup.ServerSnapshotStored, 0))

	snapshots, err := svc.ListStoredServerSnapshots(context.Background())
	is.NoErr(err)
	is.Equal(len(snapshots), 1)
	is.Equal(snapshots[0].Status, backup.ServerSnapshotStored)
}

// TestService_StartRestore_TransitionsToFailedWhenBeginFails catches bug #4:
// When beginServerRestore fails (e.g., UpdateRestoreJobStatus returns an
// error), the RestoreJob should transition to failed rather than being left
// orphaned in starting state. Currently StartRestore returns an error but
// the job stays in starting.
func TestService_StartRestore_TransitionsToFailedWhenBeginFails(t *testing.T) {
	is := is.New(t)
	cfg := backup.DefaultBackupConfig()
	cfg.S3Endpoint = "https://s3.example.com"
	cfg.S3Bucket = "test-bucket"
	cfg.S3Region = "us-east-1"

	store := fake.NewFakeBackupStore()
	s3 := fake.NewFakeS3Provider()
	bundler := fake.NewFakeBundleProvider()
	snapshotDataProvider := fake.NewFakeSnapshotDataProvider()
	clock := newFakeClock()
	repos := fake.NewFakeRepoProvider([]backup.RepoInfo{
		{Name: "repo1", DefaultBranch: "main"},
	})

	// Make UpdateRestoreJobStatus fail for non-failed targets. This simulates
	// a DB error during the starting -> restoring_server transition, but
	// allows the cleanup path (starting -> failed) to succeed.
	store.UpdateRestoreNonFailedErr = backup.ErrBackupNotFound

	svc := backup.NewBackupService(cfg, store, s3, bundler, snapshotDataProvider, repos, clock, nil)

	_, err := svc.StartRestore(context.Background(), backup.UserInfo{Role: "admin"})
	// StartRestore should handle the error and ensure the job is in failed state.
	// Currently it returns an error but leaves the job in starting.
	is.True(err != nil) // error expected when beginServerRestore fails

	// Bug #4: After the fix, the job must be in failed state.
	// Currently it stays in starting because failRestoreJob is not called.
	jobs, listErr := store.ListRestoreJobsByStatus(context.Background(), backup.RestoreJobFailed)
	is.NoErr(listErr)
	is.Equal(len(jobs), 1) // job must have been transitioned to failed

	// Also verify no job remains in starting.
	startingJobs, listErr := store.ListRestoreJobsByStatus(context.Background(), backup.RestoreJobStarting)
	is.NoErr(listErr)
	is.Equal(len(startingJobs), 0) // no orphaned jobs in starting
}
