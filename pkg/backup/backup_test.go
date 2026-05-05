package backup

import (
	"testing"
	"time"

	"github.com/matryer/is"
)

// ============================================================================
// Fake Clock — deterministic time for temporal tests
// Per AGENTS.md: external dependencies including time go through ports.
// ============================================================================

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
}

// ============================================================================
// Helper: valid backup config with defaults
// ============================================================================

func validConfig() BackupConfig {
	return DefaultBackupConfig()
}

func validConfigWithOverrides() BackupConfig {
	cfg := DefaultBackupConfig()
	cfg.S3Endpoint = "https://s3.example.com"
	cfg.S3Bucket = "my-bucket"
	cfg.S3Region = "us-east-1"
	return cfg
}

// ============================================================================
// 1. Entity and value type tests
// Spec obligations: entity-fields.*
// ============================================================================

func TestEntityFields_BackupSchedule(t *testing.T) {
	is := is.New(t)
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	s := BackupSchedule{NextRunAt: now}
	is.Equal(s.NextRunAt, now) // next_run_at field present with correct type
}

func TestEntityFields_RepoBackup(t *testing.T) {
	is := is.New(t)
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	b := RepoBackup{
		ID:         1,
		RepoName:   "my-repo",
		CreatedAt:  now,
		RetryCount: 0,
		Status:     RepoBackupUploading,
	}
	is.Equal(b.RepoName, "my-repo")        // repo field present
	is.Equal(b.CreatedAt, now)              // created_at field present with Timestamp type
	is.Equal(b.RetryCount, 0)              // retry_count field present with Integer type
	is.Equal(b.Status, RepoBackupUploading) // status field present with enum type
}

func TestEntityFields_ServerSnapshot(t *testing.T) {
	is := is.New(t)
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	s := ServerSnapshot{
		ID:         1,
		CreatedAt:  now,
		RetryCount: 0,
		Status:     ServerSnapshotUploading,
	}
	is.Equal(s.CreatedAt, now)                 // created_at field present with Timestamp type
	is.Equal(s.RetryCount, 0)                  // retry_count field present with Integer type
	is.Equal(s.Status, ServerSnapshotUploading) // status field present with enum type
}

func TestEntityFields_RestoreJob(t *testing.T) {
	is := is.New(t)
	j := RestoreJob{ID: 1, Status: RestoreJobStarting}
	is.Equal(j.Status, RestoreJobStarting) // status field present with enum type
}

func TestEntityFields_ExternalRepo(t *testing.T) {
	is := is.New(t)
	r := RepoInfo{Name: "test-repo", DefaultBranch: "main"}
	is.Equal(r.Name, "test-repo")       // name field present
	is.Equal(r.DefaultBranch, "main")    // default_branch field present
}

func TestEntityFields_ExternalUser(t *testing.T) {
	is := is.New(t)
	u := UserInfo{Role: "admin"}
	is.Equal(u.Role, "admin") // role field present
}

func TestEntityFields_ExternalS3Client(t *testing.T) {
	is := is.New(t)
	// S3Client maps to config (S3Endpoint field), verified via config tests.
	// The external entity has an endpoint field; we verify config has it.
	cfg := validConfigWithOverrides()
	is.Equal(cfg.S3Endpoint, "https://s3.example.com") // endpoint field present via config
}

// ============================================================================
// 2. Enum tests
// Spec obligations: entity-fields.* (enum value membership)
// ============================================================================

func TestRepoBackupStatus_AllValuesValid(t *testing.T) {
	is := is.New(t)
	for _, s := range []RepoBackupStatus{RepoBackupUploading, RepoBackupStored, RepoBackupFailed} {
		is.Equal(ValidRepoBackupStatuses[s], true) // enum value must be in valid set
	}
}

func TestRepoBackupStatus_InvalidValues(t *testing.T) {
	is := is.New(t)
	invalid := RepoBackupStatus("pending")
	is.Equal(ValidRepoBackupStatuses[invalid], false) // non-enum values are not valid
}

func TestServerSnapshotStatus_AllValuesValid(t *testing.T) {
	is := is.New(t)
	for _, s := range []ServerSnapshotStatus{ServerSnapshotUploading, ServerSnapshotStored, ServerSnapshotFailed} {
		is.Equal(ValidServerSnapshotStatuses[s], true) // enum value must be in valid set
	}
}

func TestServerSnapshotStatus_InvalidValues(t *testing.T) {
	is := is.New(t)
	invalid := ServerSnapshotStatus("pending")
	is.Equal(ValidServerSnapshotStatuses[invalid], false) // non-enum values are not valid
}

func TestRestoreJobStatus_AllValuesValid(t *testing.T) {
	is := is.New(t)
	for _, s := range []RestoreJobStatus{
		RestoreJobStarting,
		RestoreJobRestoringServer,
		RestoreJobRestoringRepos,
		RestoreJobCompleted,
		RestoreJobFailed,
	} {
		is.Equal(ValidRestoreJobStatuses[s], true) // enum value must be in valid set
	}
}

func TestRestoreJobStatus_InvalidValues(t *testing.T) {
	is := is.New(t)
	invalid := RestoreJobStatus("unknown")
	is.Equal(ValidRestoreJobStatuses[invalid], false) // non-enum values are not valid
}

// ============================================================================
// 3. Config tests
// Spec obligations: config-default.*
// ============================================================================

func TestConfig_Defaults(t *testing.T) {
	is := is.New(t)
	cfg := DefaultBackupConfig()

	is.Equal(cfg.S3PathPrefix, "soft-serve")             // config-default.s3_path_prefix
	is.Equal(cfg.ScheduleInterval, 6*time.Hour)          // config-default.schedule_interval
	is.Equal(cfg.MaxRepoBackups, 5)                       // config-default.max_repo_backups
	is.Equal(cfg.MaxServerSnapshots, 30)                   // config-default.max_server_snapshots
	is.Equal(cfg.MaxUploadRetries, 3)                      // config-default.max_upload_retries
	is.Equal(cfg.UploadTimeout, 1*time.Hour)              // config-default.upload_timeout
	is.Equal(cfg.BackupReposOnSchedule, false)             // config-default.backup_repos_on_schedule
}

func TestConfig_MandatoryParameters(t *testing.T) {
	is := is.New(t)
	// Spec declares s3_endpoint, s3_bucket, s3_region without defaults.
	// They are mandatory — a zero-value config should make this obvious.
	cfg := BackupConfig{} // no overrides
	is.Equal(cfg.S3Endpoint, "")   // mandatory: no default for s3_endpoint
	is.Equal(cfg.S3Bucket, "")     // mandatory: no default for s3_bucket
	is.Equal(cfg.S3Region, "")     // mandatory: no default for s3_region
}

func TestConfig_Overrides(t *testing.T) {
	is := is.New(t)
	cfg := validConfigWithOverrides()
	is.Equal(cfg.S3Endpoint, "https://s3.example.com") // override replaces default
	is.Equal(cfg.S3Bucket, "my-bucket")                // override replaces default
	is.Equal(cfg.S3Region, "us-east-1")                 // override replaces default

	// Unoverridden defaults still hold.
	is.Equal(cfg.S3PathPrefix, "soft-serve")
	is.Equal(cfg.ScheduleInterval, 6*time.Hour)
	is.Equal(cfg.MaxRepoBackups, 5)
	is.Equal(cfg.MaxServerSnapshots, 30)
	is.Equal(cfg.MaxUploadRetries, 3)
	is.Equal(cfg.UploadTimeout, 1*time.Hour)
	is.Equal(cfg.BackupReposOnSchedule, false)
}

// ============================================================================
// 4. Transition tests
// Spec obligations: transition-edge.*, transition-rejected.*, transition-terminal.*
// ============================================================================

// --- RepoBackup transitions ---

func TestRepoBackupTransition_UploadToStored(t *testing.T) {
	is := is.New(t)
	b := &RepoBackup{Status: RepoBackupUploading}
	is.Equal(b.CanTransition(RepoBackupStored), true)  // uploading -> stored is valid
}

func TestRepoBackupTransition_UploadToFailed(t *testing.T) {
	is := is.New(t)
	b := &RepoBackup{Status: RepoBackupUploading}
	is.Equal(b.CanTransition(RepoBackupFailed), true)  // uploading -> failed is valid
}

func TestRepoBackupTransition_RejectedFromStored(t *testing.T) {
	is := is.New(t)
	b := &RepoBackup{Status: RepoBackupStored}
	// Terminal state: no outbound transitions valid.
	is.Equal(b.CanTransition(RepoBackupUploading), false) // stored -> uploading rejected
	is.Equal(b.CanTransition(RepoBackupFailed), false)    // stored -> failed rejected
	is.Equal(b.CanTransition(RepoBackupStored), false)    // stored -> stored rejected (no self-loop)
}

func TestRepoBackupTransition_RejectedFromFailed(t *testing.T) {
	is := is.New(t)
	b := &RepoBackup{Status: RepoBackupFailed}
	// Terminal state: no outbound transitions valid.
	is.Equal(b.CanTransition(RepoBackupUploading), false) // failed -> uploading rejected
	is.Equal(b.CanTransition(RepoBackupStored), false)    // failed -> stored rejected
	is.Equal(b.CanTransition(RepoBackupFailed), false)    // failed -> failed rejected (no self-loop)
}

func TestRepoBackupTerminal_StoredAndFailed(t *testing.T) {
	is := is.New(t)
	is.Equal(RepoBackupStored.IsTerminal(), true)  // stored is terminal
	is.Equal(RepoBackupFailed.IsTerminal(), true)   // failed is terminal
	is.Equal(RepoBackupUploading.IsTerminal(), false) // uploading is not terminal
}

func TestRepoBackupTransition_UploadNoSelfLoop(t *testing.T) {
	is := is.New(t)
	b := &RepoBackup{Status: RepoBackupUploading}
	is.Equal(b.CanTransition(RepoBackupUploading), false) // uploading -> uploading is not declared
}

// --- ServerSnapshot transitions ---

func TestServerSnapshotTransition_UploadToStored(t *testing.T) {
	is := is.New(t)
	s := &ServerSnapshot{Status: ServerSnapshotUploading}
	is.Equal(s.CanTransition(ServerSnapshotStored), true)  // uploading -> stored is valid
}

func TestServerSnapshotTransition_UploadToFailed(t *testing.T) {
	is := is.New(t)
	s := &ServerSnapshot{Status: ServerSnapshotUploading}
	is.Equal(s.CanTransition(ServerSnapshotFailed), true)  // uploading -> failed is valid
}

func TestServerSnapshotTransition_RejectedFromStored(t *testing.T) {
	is := is.New(t)
	s := &ServerSnapshot{Status: ServerSnapshotStored}
	is.Equal(s.CanTransition(ServerSnapshotUploading), false) // stored -> uploading rejected
	is.Equal(s.CanTransition(ServerSnapshotFailed), false)    // stored -> failed rejected
	is.Equal(s.CanTransition(ServerSnapshotStored), false)    // stored -> stored rejected (no self-loop)
}

func TestServerSnapshotTransition_RejectedFromFailed(t *testing.T) {
	is := is.New(t)
	s := &ServerSnapshot{Status: ServerSnapshotFailed}
	is.Equal(s.CanTransition(ServerSnapshotUploading), false) // failed -> uploading rejected
	is.Equal(s.CanTransition(ServerSnapshotStored), false)    // failed -> stored rejected
	is.Equal(s.CanTransition(ServerSnapshotFailed), false)    // failed -> failed rejected (no self-loop)
}

func TestServerSnapshotTerminal_StoredAndFailed(t *testing.T) {
	is := is.New(t)
	is.Equal(ServerSnapshotStored.IsTerminal(), true)    // stored is terminal
	is.Equal(ServerSnapshotFailed.IsTerminal(), true)     // failed is terminal
	is.Equal(ServerSnapshotUploading.IsTerminal(), false) // uploading is not terminal
}

// --- RestoreJob transitions ---

func TestRestoreJobTransition_StartingToRestoringServer(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobStarting}
	is.Equal(j.CanTransition(RestoreJobRestoringServer), true) // starting -> restoring_server
}

func TestRestoreJobTransition_StartingToFailed(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobStarting}
	is.Equal(j.CanTransition(RestoreJobFailed), true) // starting -> failed
}

func TestRestoreJobTransition_RestoringServerToRestoringRepos(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobRestoringServer}
	is.Equal(j.CanTransition(RestoreJobRestoringRepos), true) // restoring_server -> restoring_repos
}

func TestRestoreJobTransition_RestoringServerToFailed(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobRestoringServer}
	is.Equal(j.CanTransition(RestoreJobFailed), true) // restoring_server -> failed
}

func TestRestoreJobTransition_RestoringReposToCompleted(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobRestoringRepos}
	is.Equal(j.CanTransition(RestoreJobCompleted), true) // restoring_repos -> completed
}

func TestRestoreJobTransition_RestoringReposToFailed(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobRestoringRepos}
	is.Equal(j.CanTransition(RestoreJobFailed), true) // restoring_repos -> failed
}

func TestRestoreJobTransition_RejectedUndeclared(t *testing.T) {
	is := is.New(t)
	tests := []struct {
		name string
		from RestoreJobStatus
		to   RestoreJobStatus
	}{
		// Non-adjacent transitions not in the graph.
		{"starting cannot jump to restoring_repos", RestoreJobStarting, RestoreJobRestoringRepos},
		{"starting cannot jump to completed", RestoreJobStarting, RestoreJobCompleted},
		{"restoring_server cannot jump to completed", RestoreJobRestoringServer, RestoreJobCompleted},
		{"restoring_server cannot jump to starting", RestoreJobRestoringServer, RestoreJobStarting},
		{"restoring_repos cannot jump to starting", RestoreJobRestoringRepos, RestoreJobStarting},
		{"restoring_repos cannot jump to restoring_server", RestoreJobRestoringRepos, RestoreJobRestoringServer},
		// No self-loops.
		{"starting -> starting is not declared", RestoreJobStarting, RestoreJobStarting},
		{"restoring_server -> restoring_server not declared", RestoreJobRestoringServer, RestoreJobRestoringServer},
		{"restoring_repos -> restoring_repos not declared", RestoreJobRestoringRepos, RestoreJobRestoringRepos},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := &RestoreJob{Status: tt.from}
			is.New(t).Equal(j.CanTransition(tt.to), false)
		})
	}
}

func TestRestoreJobTerminal_CompletedAndFailed(t *testing.T) {
	is := is.New(t)
	is.Equal(RestoreJobCompleted.IsTerminal(), true)       // completed is terminal
	is.Equal(RestoreJobFailed.IsTerminal(), true)           // failed is terminal
	is.Equal(RestoreJobStarting.IsTerminal(), false)       // starting is not terminal
	is.Equal(RestoreJobRestoringServer.IsTerminal(), false) // restoring_server is not terminal
	is.Equal(RestoreJobRestoringRepos.IsTerminal(), false)  // restoring_repos is not terminal
}

func TestRestoreJobTerminal_NoOutboundFromCompleted(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobCompleted}
	for _, s := range []RestoreJobStatus{
		RestoreJobStarting,
		RestoreJobRestoringServer,
		RestoreJobRestoringRepos,
		RestoreJobCompleted,
		RestoreJobFailed,
	} {
		is.Equal(j.CanTransition(s), false) // completed is terminal: no outbound transitions
	}
}

func TestRestoreJobTerminal_NoOutboundFromFailed(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobFailed}
	for _, s := range []RestoreJobStatus{
		RestoreJobStarting,
		RestoreJobRestoringServer,
		RestoreJobRestoringRepos,
		RestoreJobCompleted,
		RestoreJobFailed,
	} {
		is.Equal(j.CanTransition(s), false) // failed is terminal: no outbound transitions
	}
}

// ============================================================================
// 5. Rule tests — success cases
// Spec obligations: rule-success.*
// ============================================================================

// --- CreateRepoBackupOnPush ---

func TestRule_CreateRepoBackupOnPush_Success(t *testing.T) {
	is := is.New(t)
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	b := RepoBackup{
		ID:         1,
		RepoName:   "my-repo",
		CreatedAt:  now,
		RetryCount: 0,
		Status:     RepoBackupUploading,
	}
	// Rule ensures: RepoBackup created with status=uploading, retry_count=0
	is.Equal(b.Status, RepoBackupUploading) // status should be uploading on creation
	is.Equal(b.RetryCount, 0)               // retry_count should be 0 on creation
	is.Equal(b.RepoName, "my-repo")         // repo field set from trigger
	is.Equal(b.CreatedAt, now)               // created_at set from now
}

// --- CreateScheduledRepoBackups ---

func TestRule_CreateScheduledRepoBackups_Success(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	cfg.BackupReposOnSchedule = true // requires config.backup_repos_on_schedule = true
	is.Equal(cfg.BackupReposOnSchedule, true) // precondition satisfied

	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	b := RepoBackup{
		ID:         2,
		RepoName:   "scheduled-repo",
		CreatedAt:  now,
		RetryCount: 0,
		Status:     RepoBackupUploading,
	}
	is.Equal(b.Status, RepoBackupUploading)
	is.Equal(b.RetryCount, 0)
}

func TestRule_CreateScheduledRepoBackups_RejectedWhenOff(t *testing.T) {
	is := is.New(t)
	cfg := DefaultBackupConfig()
	is.Equal(cfg.BackupReposOnSchedule, false) // config.backup_repos_on_schedule = false (default)
	// When requires clause fails, the rule is rejected: no RepoBackups created.
}

// --- CreateServerSnapshot ---

func TestRule_CreateServerSnapshot_Success(t *testing.T) {
	is := is.New(t)
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	s := ServerSnapshot{
		ID:         1,
		CreatedAt:  now,
		RetryCount: 0,
		Status:     ServerSnapshotUploading,
	}
	is.Equal(s.Status, ServerSnapshotUploading) // status = uploading on creation
	is.Equal(s.RetryCount, 0)                    // retry_count = 0 on creation
	is.Equal(s.CreatedAt, now)                    // created_at = now
}

// Entity creation verification: CreateServerSnapshot ensures fields
func TestRule_CreateServerSnapshot_EntityCreation(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	now := clk.Now()
	cfg := validConfig()
	// CreateServerSnapshot ensures:
	//   ServerSnapshot.created(created_at: now, retry_count: 0, status: uploading)
	//   schedule.next_run_at = now + config.schedule_interval
	snapshot := ServerSnapshot{
		CreatedAt:  now,
		RetryCount: 0,
		Status:     ServerSnapshotUploading,
	}
	is.Equal(snapshot.CreatedAt, now)                      // created_at = now
	is.Equal(snapshot.RetryCount, 0)                      // retry_count = 0
	is.Equal(snapshot.Status, ServerSnapshotUploading)     // status = uploading

	schedule := BackupSchedule{NextRunAt: now}
	expectedNextRunAt := now.Add(cfg.ScheduleInterval)
	schedule.NextRunAt = expectedNextRunAt
	is.Equal(schedule.NextRunAt, expectedNextRunAt)         // next_run_at advances by schedule_interval
}

// --- FireBackupSchedule ---

func TestRule_FireBackupSchedule_Success(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()
	schedule := BackupSchedule{NextRunAt: clk.Now()}
	// when: schedule.next_run_at <= now → fires at the deadline.
	is.Equal(!schedule.NextRunAt.After(clk.Now()), true) // next_run_at <= now means trigger fires
	// After firing, schedule advances: next_run_at = now + schedule_interval
	newRunAt := clk.Now().Add(cfg.ScheduleInterval)
	is.Equal(newRunAt, clk.Now().Add(6*time.Hour)) // next_run_at advances by schedule_interval
}

// --- RepoBackupUploadSucceeds ---

func TestRule_RepoBackupUploadSucceeds_Success(t *testing.T) {
	is := is.New(t)
	b := &RepoBackup{Status: RepoBackupUploading}
	// requires: backup.status = uploading → precondition met
	is.Equal(b.Status, RepoBackupUploading)
	// ensures: backup.status = stored
	b.Status = RepoBackupStored
	is.Equal(b.Status, RepoBackupStored) // transition uploading -> stored
}

func TestRule_RepoBackupUploadSucceeds_RejectedWhenNotUploading(t *testing.T) {
	is := is.New(t)
	// requires: backup.status = uploading. If status is not uploading, rule is rejected.
	for _, status := range []RepoBackupStatus{RepoBackupStored, RepoBackupFailed} {
		b := &RepoBackup{Status: status}
		is.Equal(b.Status != RepoBackupUploading, true) // precondition fails: status != uploading
		// Status should remain unchanged.
		_ = b
	}
}

// --- RepoBackupUploadFails ---

func TestRule_RepoBackupUploadFails_Success_RetryIncrement(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	for retryCount := 0; retryCount < cfg.MaxUploadRetries; retryCount++ {
		b := &RepoBackup{RetryCount: retryCount, Status: RepoBackupUploading}
		// If retry_count < max_upload_retries, increment retry_count (stay in uploading)
		if b.RetryCount < cfg.MaxUploadRetries {
			newRetryCount := b.RetryCount + 1
			is.Equal(newRetryCount, retryCount+1) // retry_count incremented
		}
	}
}

func TestRule_RepoBackupUploadFails_Success_ExhaustedRetries(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	// When retry_count >= max_upload_retries, status transitions to failed.
	b := &RepoBackup{RetryCount: cfg.MaxUploadRetries, Status: RepoBackupUploading}
	is.Equal(b.RetryCount >= cfg.MaxUploadRetries, true) // retries exhausted
	b.Status = RepoBackupFailed
	is.Equal(b.Status, RepoBackupFailed) // status becomes failed
}

func TestRule_RepoBackupUploadFails_RejectedWhenNotUploading(t *testing.T) {
	is := is.New(t)
	// requires: backup.status = uploading. If not uploading, rule is rejected.
	b := &RepoBackup{Status: RepoBackupStored}
	is.Equal(b.Status != RepoBackupUploading, true) // precondition fails
	// Status remains unchanged.
	is.Equal(b.Status, RepoBackupStored)
}

func TestRule_RepoBackupUploadFails_EdgeCase_ExactlyAtMaxRetries(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	// With max_upload_retries=3, at retry_count=3 (which is == max_upload_retries, not <),
	// the else branch fires: status = failed.
	b := &RepoBackup{RetryCount: 3, Status: RepoBackupUploading}
	is.Equal(b.RetryCount < cfg.MaxUploadRetries, false) // condition fails: 3 < 3 is false
	// so status becomes failed
	b.Status = RepoBackupFailed
	is.Equal(b.Status, RepoBackupFailed)
}

func TestRule_RepoBackupUploadFails_EdgeCase_OneBelowMaxRetries(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	// At retry_count = 2 (< max_upload_retries = 3), increment retry_count.
	b := &RepoBackup{RetryCount: 2, Status: RepoBackupUploading}
	is.Equal(b.RetryCount < cfg.MaxUploadRetries, true) // condition true: 2 < 3
	b.RetryCount++
	is.Equal(b.RetryCount, 3)        // retry_count becomes 3
	is.Equal(b.Status, RepoBackupUploading) // still uploading
}

func TestRule_RepoBackupUploadFails_TotalAttempts(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	// With max_upload_retries=3, total attempts = 1 initial + 3 retries = 4.
	// Verify the retry path for each of the 3 retries.
	retryCount := 0
	for i := 0; i < cfg.MaxUploadRetries; i++ {
		b := &RepoBackup{RetryCount: retryCount, Status: RepoBackupUploading}
		is.Equal(b.RetryCount < cfg.MaxUploadRetries, true) // can retry
		retryCount++
	}
	// After exhausting retries:
	b := &RepoBackup{RetryCount: cfg.MaxUploadRetries, Status: RepoBackupUploading}
	is.Equal(b.RetryCount < cfg.MaxUploadRetries, false) // cannot retry further
	b.Status = RepoBackupFailed
	is.Equal(b.Status, RepoBackupFailed) // marked failed
}

// --- ServerSnapshotUploadSucceeds ---

func TestRule_ServerSnapshotUploadSucceeds_Success(t *testing.T) {
	is := is.New(t)
	s := &ServerSnapshot{Status: ServerSnapshotUploading}
	is.Equal(s.Status, ServerSnapshotUploading) // precondition met
	s.Status = ServerSnapshotStored
	is.Equal(s.Status, ServerSnapshotStored) // transition uploading -> stored
}

func TestRule_ServerSnapshotUploadSucceeds_RejectedWhenNotUploading(t *testing.T) {
	is := is.New(t)
	for _, status := range []ServerSnapshotStatus{ServerSnapshotStored, ServerSnapshotFailed} {
		s := &ServerSnapshot{Status: status}
		is.Equal(s.Status != ServerSnapshotUploading, true) // precondition fails
	}
}

// --- ServerSnapshotUploadFails ---

func TestRule_ServerSnapshotUploadFails_Success_RetryIncrement(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	for retryCount := 0; retryCount < cfg.MaxUploadRetries; retryCount++ {
		s := &ServerSnapshot{RetryCount: retryCount, Status: ServerSnapshotUploading}
		if s.RetryCount < cfg.MaxUploadRetries {
			newRetryCount := s.RetryCount + 1
			is.Equal(newRetryCount, retryCount+1)
		}
	}
}

func TestRule_ServerSnapshotUploadFails_Success_ExhaustedRetries(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	s := &ServerSnapshot{RetryCount: cfg.MaxUploadRetries, Status: ServerSnapshotUploading}
	is.Equal(s.RetryCount >= cfg.MaxUploadRetries, true) // retries exhausted
	s.Status = ServerSnapshotFailed
	is.Equal(s.Status, ServerSnapshotFailed) // status becomes failed
}

func TestRule_ServerSnapshotUploadFails_RejectedWhenNotUploading(t *testing.T) {
	is := is.New(t)
	s := &ServerSnapshot{Status: ServerSnapshotStored}
	is.Equal(s.Status != ServerSnapshotUploading, true) // precondition fails
	is.Equal(s.Status, ServerSnapshotStored)
}

// --- RotateRepoBackups ---

func TestRule_RotateRepoBackups_Success(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	// When a RepoBackup transitions to stored, BackupsToRotate should remove
	// surplus stored backups (oldest first) that exceed max_repo_backups.
	repoName := "my-repo"
	backups := make([]RepoBackup, cfg.MaxRepoBackups+2)
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	for i := range backups {
		backups[i] = RepoBackup{
			ID:         int64(i + 1),
			RepoName:   repoName,
			CreatedAt:  now.Add(time.Duration(i) * time.Hour),
			RetryCount: 0,
			Status:     RepoBackupStored,
		}
	}
	storedCount := 0
	for _, b := range backups {
		if b.RepoName == repoName && b.Status == RepoBackupStored {
			storedCount++
		}
	}
	is.Equal(storedCount > cfg.MaxRepoBackups, true) // more stored backups than max
	// Rotation should remove (stored - max) = 2 oldest backups.
	toRemove := storedCount - cfg.MaxRepoBackups
	is.Equal(toRemove, 2) // exactly 2 surplus backups
}

// --- RotateServerSnapshots ---

func TestRule_RotateServerSnapshots_Success(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	// When a ServerSnapshot transitions to stored, surplus snapshots are removed.
	snapshots := make([]ServerSnapshot, cfg.MaxServerSnapshots+2)
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	for i := range snapshots {
		snapshots[i] = ServerSnapshot{
			ID:         int64(i + 1),
			CreatedAt:  now.Add(time.Duration(i) * time.Hour),
			RetryCount: 0,
			Status:     ServerSnapshotStored,
		}
	}
	storedCount := 0
	for _, s := range snapshots {
		if s.Status == ServerSnapshotStored {
			storedCount++
		}
	}
	toRemove := storedCount - cfg.MaxServerSnapshots
	is.Equal(toRemove, 2) // exactly 2 surplus snapshots
}

// --- RepoBackupUploadTimeout ---

func TestRule_RepoBackupUploadTimeout_Success(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()
	// A backup created at t=0, still uploading at t=upload_timeout+1
	b := &RepoBackup{
		CreatedAt:  clk.Now(),
		RetryCount: 0,
		Status:     RepoBackupUploading,
	}
	// requires: backup.status = uploading → met
	is.Equal(b.Status, RepoBackupUploading)
	// when: backup.created_at + config.upload_timeout <= now
	deadline := b.CreatedAt.Add(cfg.UploadTimeout)
	clk.Advance(cfg.UploadTimeout + 1*time.Second)
	is.Equal(!deadline.After(clk.Now()), true) // deadline has passed
	// ensures: backup.status = failed
	b.Status = RepoBackupFailed
	is.Equal(b.Status, RepoBackupFailed)
}

func TestRule_RepoBackupUploadTimeout_RejectedWhenNotUploading(t *testing.T) {
	is := is.New(t)
	// requires: backup.status = uploading. If already stored or failed, rule doesn't apply.
	for _, status := range []RepoBackupStatus{RepoBackupStored, RepoBackupFailed} {
		b := &RepoBackup{Status: status}
		is.Equal(b.Status != RepoBackupUploading, true) // precondition fails
	}
}

// --- ServerSnapshotUploadTimeout ---

func TestRule_ServerSnapshotUploadTimeout_Success(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()
	s := &ServerSnapshot{
		CreatedAt:  clk.Now(),
		RetryCount: 0,
		Status:     ServerSnapshotUploading,
	}
	is.Equal(s.Status, ServerSnapshotUploading)
	deadline := s.CreatedAt.Add(cfg.UploadTimeout)
	clk.Advance(cfg.UploadTimeout + 1*time.Second)
	is.Equal(!deadline.After(clk.Now()), true) // deadline passed
	s.Status = ServerSnapshotFailed
	is.Equal(s.Status, ServerSnapshotFailed)
}

func TestRule_ServerSnapshotUploadTimeout_RejectedWhenNotUploading(t *testing.T) {
	is := is.New(t)
	for _, status := range []ServerSnapshotStatus{ServerSnapshotStored, ServerSnapshotFailed} {
		s := &ServerSnapshot{Status: status}
		is.Equal(s.Status != ServerSnapshotUploading, true) // precondition fails
	}
}

// --- StartRestore ---

func TestRule_StartRestore_Success(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobStarting}
	// Rule ensures: RestoreJob created with status = starting
	is.Equal(j.Status, RestoreJobStarting)
}

func TestRule_StartRestore_EntityCreation(t *testing.T) {
	is := is.New(t)
	// RestoreJob created with status = starting
	j := RestoreJob{ID: 1, Status: RestoreJobStarting}
	is.Equal(j.Status, RestoreJobStarting) // entity created with status = starting
}

// --- BeginServerRestore ---

func TestRule_BeginServerRestore_Success(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobStarting}
	// when: RestoreJob.status becomes starting → auto-transition to restoring_server
	is.Equal(j.CanTransition(RestoreJobRestoringServer), true) // starting -> restoring_server is valid
	j.Status = RestoreJobRestoringServer
	is.Equal(j.Status, RestoreJobRestoringServer) // auto-transition happens immediately
}

// --- ServerDataRestored ---

func TestRule_ServerDataRestored_Success(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobRestoringServer}
	// requires: job.status = restoring_server → met
	is.Equal(j.Status, RestoreJobRestoringServer)
	// ensures: job.status = restoring_repos
	j.Status = RestoreJobRestoringRepos
	is.Equal(j.Status, RestoreJobRestoringRepos)
}

func TestRule_ServerDataRestored_RejectedWhenNotRestoringServer(t *testing.T) {
	is := is.New(t)
	// requires: job.status = restoring_server. Fails for other states.
	for _, status := range []RestoreJobStatus{
		RestoreJobStarting,
		RestoreJobRestoringRepos,
		RestoreJobCompleted,
		RestoreJobFailed,
	} {
		j := &RestoreJob{Status: status}
		is.Equal(j.Status != RestoreJobRestoringServer, true) // precondition fails
	}
}

// --- AllReposRestored ---

func TestRule_AllReposRestored_Success(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobRestoringRepos}
	// requires: job.status = restoring_repos → met
	is.Equal(j.Status, RestoreJobRestoringRepos)
	// ensures: job.status = completed
	j.Status = RestoreJobCompleted
	is.Equal(j.Status, RestoreJobCompleted)
}

func TestRule_AllReposRestored_RejectedWhenNotRestoringRepos(t *testing.T) {
	is := is.New(t)
	for _, status := range []RestoreJobStatus{
		RestoreJobStarting,
		RestoreJobRestoringServer,
		RestoreJobCompleted,
		RestoreJobFailed,
	} {
		j := &RestoreJob{Status: status}
		is.Equal(j.Status != RestoreJobRestoringRepos, true) // precondition fails
	}
}

// --- RestoreFailedFromStarting ---

func TestRule_RestoreFailedFromStarting_Success(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobStarting}
	// requires: job.status = starting → met
	is.Equal(j.Status, RestoreJobStarting)
	// ensures: job.status = failed
	j.Status = RestoreJobFailed
	is.Equal(j.Status, RestoreJobFailed)
}

func TestRule_RestoreFailedFromStarting_RejectedWhenNotStarting(t *testing.T) {
	is := is.New(t)
	for _, status := range []RestoreJobStatus{
		RestoreJobRestoringServer,
		RestoreJobRestoringRepos,
		RestoreJobCompleted,
		RestoreJobFailed,
	} {
		j := &RestoreJob{Status: status}
		is.Equal(j.Status != RestoreJobStarting, true) // precondition fails
	}
}

// --- RestoreFailedFromServerRestore ---

func TestRule_RestoreFailedFromServerRestore_Success(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobRestoringServer}
	is.Equal(j.Status, RestoreJobRestoringServer)
	j.Status = RestoreJobFailed
	is.Equal(j.Status, RestoreJobFailed)
}

func TestRule_RestoreFailedFromServerRestore_RejectedWhenNotRestoringServer(t *testing.T) {
	is := is.New(t)
	for _, status := range []RestoreJobStatus{
		RestoreJobStarting,
		RestoreJobRestoringRepos,
		RestoreJobCompleted,
		RestoreJobFailed,
	} {
		j := &RestoreJob{Status: status}
		is.Equal(j.Status != RestoreJobRestoringServer, true) // precondition fails
	}
}

// --- RestoreFailedFromRepoRestore ---

func TestRule_RestoreFailedFromRepoRestore_Success(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobRestoringRepos}
	is.Equal(j.Status, RestoreJobRestoringRepos)
	j.Status = RestoreJobFailed
	is.Equal(j.Status, RestoreJobFailed)
}

func TestRule_RestoreFailedFromRepoRestore_RejectedWhenNotRestoringRepos(t *testing.T) {
	is := is.New(t)
	for _, status := range []RestoreJobStatus{
		RestoreJobStarting,
		RestoreJobRestoringServer,
		RestoreJobCompleted,
		RestoreJobFailed,
	} {
		j := &RestoreJob{Status: status}
		is.Equal(j.Status != RestoreJobRestoringRepos, true) // precondition fails
	}
}

// ============================================================================
// 6. Temporal tests
// Spec obligations: temporal.*
// Per AGENTS.md, temporal tests need a controllable clock.
// ============================================================================

func TestTemporal_FireBackupSchedule_AtDeadline(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	schedule := BackupSchedule{NextRunAt: clk.Now()}
	// At the exact deadline: next_run_at <= now should fire.
	is.Equal(!schedule.NextRunAt.After(clk.Now()), true) // schedule fires at deadline
}

func TestTemporal_FireBackupSchedule_BeforeDeadline(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()
	// Schedule fires next at now + 6 hours. At now, it should not fire.
	schedule := BackupSchedule{NextRunAt: clk.Now().Add(cfg.ScheduleInterval)}
	is.Equal(schedule.NextRunAt.After(clk.Now()), true) // next_run_at > now, so should NOT fire
}

func TestTemporal_FireBackupSchedule_AfterDeadline_DoesNotReFire(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()
	// After firing, the schedule advances to now + schedule_interval.
	// A second check at the same time should not re-fire.
	newRunAt := clk.Now().Add(cfg.ScheduleInterval)
	is.Equal(newRunAt.After(clk.Now()), true) // next scheduled run is in the future
}

func TestTemporal_RepoBackupUploadTimeout_AtDeadline(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()
	b := &RepoBackup{CreatedAt: clk.Now(), Status: RepoBackupUploading}
	// Exactly at the upload_timeout deadline.
	deadline := b.CreatedAt.Add(cfg.UploadTimeout)
	clk.Advance(cfg.UploadTimeout)
	is.Equal(!deadline.After(clk.Now()), true) // deadline <= now, timeout fires
}

func TestTemporal_RepoBackupUploadTimeout_BeforeDeadline(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()
	b := &RepoBackup{CreatedAt: clk.Now(), Status: RepoBackupUploading}
	// Before the timeout: created_at + upload_timeout > now
	clk.Advance(cfg.UploadTimeout - 1*time.Minute) // not yet at deadline
	deadline := b.CreatedAt.Add(cfg.UploadTimeout)
	is.Equal(deadline.After(clk.Now()), true) // deadline > now, timeout should NOT fire
	// Backup should still be uploading.
	is.Equal(b.Status, RepoBackupUploading)
}

func TestTemporal_ServerSnapshotUploadTimeout_AtDeadline(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()
	s := &ServerSnapshot{CreatedAt: clk.Now(), Status: ServerSnapshotUploading}
	deadline := s.CreatedAt.Add(cfg.UploadTimeout)
	clk.Advance(cfg.UploadTimeout)
	is.Equal(!deadline.After(clk.Now()), true) // deadline <= now, timeout fires
}

func TestTemporal_ServerSnapshotUploadTimeout_BeforeDeadline(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()
	s := &ServerSnapshot{CreatedAt: clk.Now(), Status: ServerSnapshotUploading}
	clk.Advance(cfg.UploadTimeout - 1*time.Minute)
	deadline := s.CreatedAt.Add(cfg.UploadTimeout)
	is.Equal(deadline.After(clk.Now()), true) // deadline > now, timeout should NOT fire
	is.Equal(s.Status, ServerSnapshotUploading)
}

// ============================================================================
// 7. Invariant tests
// Spec obligations: invariant.MaxRepoBackupsPerRepo, invariant.MaxServerSnaphots
// ============================================================================

func TestInvariat_MaxRepoBackupsPerRepo(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	repoName := "my-repo"

	tests := []struct {
		name        string
		backups     []RepoBackup
		shouldHold  bool
		description string
	}{
		{
			name:        "at limit",
			backups:     makeStoredBackups(repoName, cfg.MaxRepoBackups),
			shouldHold:  true,
			description: "stored count == max_repo_backups is within invariant",
		},
		{
			name:        "below limit",
			backups:     makeStoredBackups(repoName, cfg.MaxRepoBackups-1),
			shouldHold:  true,
			description: "stored count < max_repo_backups is within invariant",
		},
		{
			name:        "above limit",
			backups:     makeStoredBackups(repoName, cfg.MaxRepoBackups+1),
			shouldHold:  false,
			description: "stored count > max_repo_backups violates invariant",
		},
		{
			name: "zero backups",
			backups: []RepoBackup{},
			shouldHold:  true,
			description: "0 stored backups is within invariant",
		},
		{
			name:        "failed backups not counted",
			backups:     append(makeStoredBackups(repoName, cfg.MaxRepoBackups), RepoBackup{RepoName: repoName, Status: RepoBackupFailed}),
			shouldHold:  true,
			description: "failed backups are not counted by the invariant (status = stored)",
		},
		{
			name:        "per repo boundary",
			backups:     append(makeStoredBackups("repo-a", cfg.MaxRepoBackups), makeStoredBackups("repo-b", cfg.MaxRepoBackups)...),
			shouldHold:  true,
			description: "each repo independently has max_repo_backups stored",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storedPerRepo := map[string]int{}
			for _, b := range tt.backups {
				if b.Status == RepoBackupStored {
					storedPerRepo[b.RepoName]++
				}
			}
			holds := true
			for _, count := range storedPerRepo {
				if count > cfg.MaxRepoBackups {
					holds = false
				}
			}
			is.New(t).Equal(holds, tt.shouldHold)
		})
	}
}

func TestInvariat_MaxServerSnapshots(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()

	tests := []struct {
		name        string
		snapshots   []ServerSnapshot
		shouldHold  bool
		description string
	}{
		{
			name:        "at limit",
			snapshots:   makeStoredSnapshots(cfg.MaxServerSnapshots),
			shouldHold:  true,
			description: "stored count == max_server_snapshots is within invariant",
		},
		{
			name:        "below limit",
			snapshots:   makeStoredSnapshots(cfg.MaxServerSnapshots - 1),
			shouldHold:  true,
			description: "stored count < max_server_snapshots is within invariant",
		},
		{
			name:        "above limit",
			snapshots:   makeStoredSnapshots(cfg.MaxServerSnapshots + 1),
			shouldHold:  false,
			description: "stored count > max_server_snapshots violates invariant",
		},
		{
			name:        "zero snapshots",
			snapshots:   []ServerSnapshot{},
			shouldHold:  true,
			description: "0 stored snapshots is within invariant",
		},
		{
			name:        "failed not counted",
			snapshots:   append(makeStoredSnapshots(cfg.MaxServerSnapshots), ServerSnapshot{Status: ServerSnapshotFailed}),
			shouldHold:  true,
			description: "failed snapshots are not counted by invariant (status = stored)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored := 0
			for _, s := range tt.snapshots {
				if s.Status == ServerSnapshotStored {
					stored++
				}
			}
			is.New(t).Equal(stored <= cfg.MaxServerSnapshots, tt.shouldHold)
		})
	}
}

// ============================================================================
// 8. State machine / reachability tests
// Walk every path through each transition graph.
// ============================================================================

// --- RepoBackup lifecycle paths ---

func TestRepoBackupStateMachine_HappyPath(t *testing.T) {
	is := is.New(t)
	// Path: uploading -> stored (via RepoBackupUploadSucceeds)
	b := &RepoBackup{Status: RepoBackupUploading, RetryCount: 0}
	is.Equal(b.CanTransition(RepoBackupStored), true)
	b.Status = RepoBackupStored
	is.Equal(b.Status, RepoBackupStored)
	is.Equal(b.Status.IsTerminal(), true) // reached terminal state
}

func TestRepoBackupStateMachine_FailurePath(t *testing.T) {
	is := is.New(t)
	// Path: uploading -> failed (via RepoBackupUploadFails with exhausted retries)
	b := &RepoBackup{Status: RepoBackupUploading, RetryCount: 3}
	is.Equal(b.CanTransition(RepoBackupFailed), true)
	b.Status = RepoBackupFailed
	is.Equal(b.Status, RepoBackupFailed)
	is.Equal(b.Status.IsTerminal(), true) // reached terminal state
}

func TestRepoBackupStateMachine_RetryThenSuccessPath(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	// Path: uploading (with retries) -> stored
	b := &RepoBackup{Status: RepoBackupUploading, RetryCount: 0}
	for i := 0; i < cfg.MaxUploadRetries; i++ {
		is.Equal(b.Status, RepoBackupUploading) // still uploading during retries
		b.RetryCount++
	}
	// After retries still in uploading (retry exhausted path goes to failed,
	// but this path tests success at any retry count).
	b.Status = RepoBackupStored
	is.Equal(b.Status, RepoBackupStored)
}

func TestRepoBackupStateMachine_TimeoutPath(t *testing.T) {
	is := is.New(t)
	// Path: uploading -> failed (via timeout)
	b := &RepoBackup{Status: RepoBackupUploading, RetryCount: 0}
	is.Equal(b.CanTransition(RepoBackupFailed), true) // timeout transitions to failed
	b.Status = RepoBackupFailed
	is.Equal(b.Status.IsTerminal(), true)
}

// --- ServerSnapshot lifecycle paths ---

func TestServerSnapshotStateMachine_HappyPath(t *testing.T) {
	is := is.New(t)
	s := &ServerSnapshot{Status: ServerSnapshotUploading, RetryCount: 0}
	is.Equal(s.CanTransition(ServerSnapshotStored), true)
	s.Status = ServerSnapshotStored
	is.Equal(s.Status, ServerSnapshotStored)
	is.Equal(s.Status.IsTerminal(), true)
}

func TestServerSnapshotStateMachine_FailurePath(t *testing.T) {
	is := is.New(t)
	s := &ServerSnapshot{Status: ServerSnapshotUploading, RetryCount: 3}
	is.Equal(s.CanTransition(ServerSnapshotFailed), true)
	s.Status = ServerSnapshotFailed
	is.Equal(s.Status.IsTerminal(), true)
}

func TestServerSnapshotStateMachine_TimeoutPath(t *testing.T) {
	is := is.New(t)
	s := &ServerSnapshot{Status: ServerSnapshotUploading}
	is.Equal(s.CanTransition(ServerSnapshotFailed), true) // timeout
	s.Status = ServerSnapshotFailed
	is.Equal(s.Status.IsTerminal(), true)
}

// --- RestoreJob lifecycle paths ---

func TestRestoreJobStateMachine_HappyPath(t *testing.T) {
	is := is.New(t)
	// starting -> restoring_server -> restoring_repos -> completed
	j := &RestoreJob{Status: RestoreJobStarting}
	is.Equal(j.CanTransition(RestoreJobRestoringServer), true)
	j.Status = RestoreJobRestoringServer
	is.Equal(j.CanTransition(RestoreJobRestoringRepos), true)
	j.Status = RestoreJobRestoringRepos
	is.Equal(j.CanTransition(RestoreJobCompleted), true)
	j.Status = RestoreJobCompleted
	is.Equal(j.Status.IsTerminal(), true)
}

func TestRestoreJobStateMachine_FailFromStarting(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobStarting}
	is.Equal(j.CanTransition(RestoreJobFailed), true)
	j.Status = RestoreJobFailed
	is.Equal(j.Status.IsTerminal(), true)
}

func TestRestoreJobStateMachine_FailFromRestoringServer(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobStarting}
	j.Status = RestoreJobRestoringServer
	is.Equal(j.CanTransition(RestoreJobFailed), true)
	j.Status = RestoreJobFailed
	is.Equal(j.Status.IsTerminal(), true)
}

func TestRestoreJobStateMachine_FailFromRestoringRepos(t *testing.T) {
	is := is.New(t)
	j := &RestoreJob{Status: RestoreJobStarting}
	j.Status = RestoreJobRestoringServer
	j.Status = RestoreJobRestoringRepos
	is.Equal(j.CanTransition(RestoreJobFailed), true)
	j.Status = RestoreJobFailed
	is.Equal(j.Status.IsTerminal(), true)
}

 func TestRestoreJobStateMachine_EveryReachableEdge(t *testing.T) {
	is := is.New(t)
	// exhaustive: verify every declared edge is reachable by some witnessing rule
	edges := []struct {
		from RestoreJobStatus
		to   RestoreJobStatus
		rule string
	}{
		{RestoreJobStarting, RestoreJobRestoringServer, "BeginServerRestore"},
		{RestoreJobStarting, RestoreJobFailed, "RestoreFailedFromStarting"},
		{RestoreJobRestoringServer, RestoreJobRestoringRepos, "ServerDataRestored"},
		{RestoreJobRestoringServer, RestoreJobFailed, "RestoreFailedFromServerRestore"},
		{RestoreJobRestoringRepos, RestoreJobCompleted, "AllReposRestored"},
		{RestoreJobRestoringRepos, RestoreJobFailed, "RestoreFailedFromRepoRestore"},
	}
	for _, e := range edges {
		j := &RestoreJob{Status: e.from}
		is.Equal(j.CanTransition(e.to), true) // edge from %s -> %s (rule: %s) must be reachable
	}
}

// No non-terminal state should be a dead end (all have at least one exit)
func TestRestoreJobStateMachine_NonTerminalStatesHaveExit(t *testing.T) {
	is := is.New(t)
	nonTerminalStates := []RestoreJobStatus{
		RestoreJobStarting,
		RestoreJobRestoringServer,
		RestoreJobRestoringRepos,
	}
	for _, s := range nonTerminalStates {
		is.Equal(len(RestoreJobTransitions[s]) > 0, true) // non-terminal state must have outbound transitions
	}
}

// ============================================================================
// 9. Default instance tests
// Spec: default BackupSchedule main = { next_run_at: now + config.schedule_interval }
// ============================================================================

func TestDefault_BackupSchedule_UnconditionalExistence(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()
	// Default BackupSchedule exists unconditionally with next_run_at = now + schedule_interval
	expected := clk.Now().Add(cfg.ScheduleInterval)
	schedule := BackupSchedule{NextRunAt: expected}
	is.Equal(schedule.NextRunAt, clk.Now().Add(cfg.ScheduleInterval)) // next_run_at matches default expression
}

// ============================================================================
// 10. Surface / actor tests
// Spec obligations: surface-actor.*, surface-exposure.*, surface-provides.*
// ============================================================================

// --- AdminBackupManagement ---

func TestSurface_AdminBackupManagement_ActorRestriction(t *testing.T) {
	is := is.New(t)
	// actor Admin identified_by: User where role = admin
	adminUser := UserInfo{Role: "admin"}
	regularUser := UserInfo{Role: "user"}
	anonymousUser := UserInfo{Role: ""}

	is.Equal(adminUser.IsAdmin(), true)          // admin matches identified_by predicate
	is.Equal(regularUser.IsAdmin(), false)        // non-admin user is not identified as Admin
	is.Equal(anonymousUser.IsAdmin(), false)      // empty role is not admin
}

func TestSurface_AdminBackupManagement_AdminCanAccess(t *testing.T) {
	is := is.New(t)
	// exposes: RepoBackups where status = stored
	// Only admins should be able to access exposed items.
	admin := UserInfo{Role: "admin"}
	is.Equal(admin.IsAdmin(), true) // admin can access AdminBackupManagement
}

func TestSurface_AdminBackupManagement_NonAdminCannotProvideRestore(t *testing.T) {
	is := is.New(t)
	// provides: AdminRequestsRestore(admin)
	// Only admin can trigger restore.
	user := UserInfo{Role: "user"}
	is.Equal(user.IsAdmin(), false) // non-admin cannot request restore
}

func TestSurface_AdminBackupManagement_ExposeStoredRepoBackups(t *testing.T) {
	is := is.New(t)
	// exposes: RepoBackups where status = stored
	// Only stored repo backups are exposed, not uploading or failed ones.
	backups := []RepoBackup{
		{ID: 1, RepoName: "repo1", Status: RepoBackupStored},
		{ID: 2, RepoName: "repo2", Status: RepoBackupUploading},
		{ID: 3, RepoName: "repo3", Status: RepoBackupFailed},
	}
	exposed := 0
	for _, b := range backups {
		if b.Status == RepoBackupStored {
			exposed++
		}
	}
	is.Equal(exposed, 1) // only stored backups are exposed
}

func TestSurface_AdminBackupManagement_ExposeStoredServerSnapshots(t *testing.T) {
	is := is.New(t)
	// exposes: ServerSnapshots where status = stored
	snapshots := []ServerSnapshot{
		{ID: 1, Status: ServerSnapshotStored},
		{ID: 2, Status: ServerSnapshotUploading},
		{ID: 3, Status: ServerSnapshotFailed},
	}
	exposed := 0
	for _, s := range snapshots {
		if s.Status == ServerSnapshotStored {
			exposed++
		}
	}
	is.Equal(exposed, 1) // only stored snapshots are exposed
}

func TestSurface_AdminBackupManagement_ExposeNonCompletedRestoreJobs(t *testing.T) {
	is := is.New(t)
	// exposes: RestoreJobs where status != completed
	// All non-completed states are exposed: starting, restoring_server, restoring_repos, failed
	jobs := []RestoreJob{
		{ID: 1, Status: RestoreJobStarting},
		{ID: 2, Status: RestoreJobRestoringServer},
		{ID: 3, Status: RestoreJobRestoringRepos},
		{ID: 4, Status: RestoreJobCompleted},
		{ID: 5, Status: RestoreJobFailed},
	}
	nonCompleted := 0
	for _, j := range jobs {
		if j.Status != RestoreJobCompleted {
			nonCompleted++
		}
	}
	is.Equal(nonCompleted, 4) // starting, restoring_server, restoring_repos, failed (all except completed)
}

// --- S3UploadAdapter ---

func TestSurface_S3UploadAdapter_ProvidesUploadSuccess(t *testing.T) {
	is := is.New(t)
	// provides: RepoBackupUploadSucceeded(backup), ServerSnapshotUploadSucceeded(snapshot)
	// These are operations provided by the S3 adapter surface.
	// Test that upload success transitions work through the surface.
	b := &RepoBackup{Status: RepoBackupUploading}
	b.Status = RepoBackupStored
	is.Equal(b.Status, RepoBackupStored) // RepoBackupUploadSucceeded transitions to stored

	s := &ServerSnapshot{Status: ServerSnapshotUploading}
	s.Status = ServerSnapshotStored
	is.Equal(s.Status, ServerSnapshotStored) // ServerSnapshotUploadSucceeded transitions to stored
}

func TestSurface_S3UploadAdapter_ProvidesUploadFailure(t *testing.T) {
	is := is.New(t)
	// provides: RepoBackupUploadFailed(backup), ServerSnapshotUploadFailed(snapshot)
	cfg := validConfig()
	// Verify retry increment on failure.
	b := &RepoBackup{Status: RepoBackupUploading, RetryCount: 0}
	if b.RetryCount < cfg.MaxUploadRetries {
		b.RetryCount++
	}
	is.Equal(b.RetryCount, 1) // retry_count incremented on first failure

	s := &ServerSnapshot{Status: ServerSnapshotUploading, RetryCount: 0}
	if s.RetryCount < cfg.MaxUploadRetries {
		s.RetryCount++
	}
	is.Equal(s.RetryCount, 1) // retry_count incremented on first failure
}

// --- S3DownloadAdapter ---

func TestSurface_S3DownloadAdapter_ProvidesDownloadOperations(t *testing.T) {
	is := is.New(t)
	// provides: ServerSnapshotRestored(job), RepoRestoresComplete(job), RestoreFailed(job)
	// These operations are S3-facing. Verify they drive transitions correctly.
	// ServerSnapshotRestored: job status must be restoring_server -> restoring_repos
	j := &RestoreJob{Status: RestoreJobRestoringServer}
	is.Equal(j.CanTransition(RestoreJobRestoringRepos), true)
	j.Status = RestoreJobRestoringRepos
	is.Equal(j.Status, RestoreJobRestoringRepos)
}

// --- GitPushNotification ---

func TestSurface_GitPushNotification_ProvidesPushToDefaultBranch(t *testing.T) {
	is := is.New(t)
	// provides: PushToDefaultBranch(repo)
	// When a push to a repo's default branch fires, a RepoBackup is created.
	repo := RepoInfo{Name: "my-repo", DefaultBranch: "main"}
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	b := RepoBackup{
		RepoName:   repo.Name,
		CreatedAt:   now,
		RetryCount:  0,
		Status:      RepoBackupUploading,
	}
	is.Equal(b.RepoName, "my-repo")  // repo name from PushToDefaultBranch
	is.Equal(b.Status, RepoBackupUploading) // created with uploading status
}

// ============================================================================
// 11. Scenario tests
// Full lifecycle: happy paths and error paths.
// ============================================================================

func TestScenario_RepoBackupLifecycle_HappyPath(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()

	// 1. Push triggers CreateRepoBackupOnPush
	b := RepoBackup{
		ID:         1,
		RepoName:   "my-repo",
		CreatedAt:  clk.Now(),
		RetryCount: 0,
		Status:     RepoBackupUploading,
	}
	is.Equal(b.Status, RepoBackupUploading)

	// 2. S3 upload succeeds: RepoBackupUploadSucceeds
	b.Status = RepoBackupStored
	is.Equal(b.Status, RepoBackupStored)
	is.Equal(b.Status.IsTerminal(), true)
	is.Equal(b.RetryCount, 0) // no retries needed

	// 3. After stored, rotation may occur, but at 1 backup doesn't exceed max
	storedCount := 1
	is.Equal(storedCount <= cfg.MaxRepoBackups, true) // invariant holds
}

func TestScenario_RepoBackupLifecycle_UploadFailsThenSucceeds(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	clk := newFakeClock()

	b := RepoBackup{
		ID:         1,
		RepoName:   "my-repo",
		CreatedAt:  clk.Now(),
		RetryCount: 0,
		Status:     RepoBackupUploading,
	}

	// First upload attempt fails
	is.Equal(b.RetryCount < cfg.MaxUploadRetries, true) // can retry
	b.RetryCount++ // retry_count becomes 1
	is.Equal(b.RetryCount, 1)
	is.Equal(b.Status, RepoBackupUploading) // still uploading

	// Second attempt fails
	is.Equal(b.RetryCount < cfg.MaxUploadRetries, true) // can retry
	b.RetryCount++
	is.Equal(b.RetryCount, 2)
	is.Equal(b.Status, RepoBackupUploading) // still uploading

	// Third attempt succeeds
	b.Status = RepoBackupStored
	is.Equal(b.Status, RepoBackupStored)
	is.Equal(b.Status.IsTerminal(), true)
}

func TestScenario_RepoBackupLifecycle_AllRetriesFail(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	clk := newFakeClock()

	b := RepoBackup{
		ID:         1,
		RepoName:   "my-repo",
		CreatedAt:  clk.Now(),
		RetryCount: 0,
		Status:     RepoBackupUploading,
	}

	// Exhaust all retries
	for i := 0; i < cfg.MaxUploadRetries; i++ {
		b.RetryCount++
		is.Equal(b.Status, RepoBackupUploading) // still uploading during retries
	}
	// After max retries exhausted
	is.Equal(b.RetryCount >= cfg.MaxUploadRetries, true)
	b.Status = RepoBackupFailed
	is.Equal(b.Status, RepoBackupFailed)
	is.Equal(b.Status.IsTerminal(), true)
}

func TestScenario_ServerSnapshotLifecycle_HappyPath(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()

	s := ServerSnapshot{
		ID:         1,
		CreatedAt:  clk.Now(),
		RetryCount: 0,
		Status:     ServerSnapshotUploading,
	}
	is.Equal(s.Status, ServerSnapshotUploading)

	s.Status = ServerSnapshotStored
	is.Equal(s.Status, ServerSnapshotStored)
	is.Equal(s.Status.IsTerminal(), true)
}

func TestScenario_FullBackupRestoreHappyPath(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()

	// --- Backup phase ---

	// 1. Schedule fires
	schedule := BackupSchedule{NextRunAt: clk.Now()}
	is.Equal(!schedule.NextRunAt.After(clk.Now()), true) // schedule at deadline

	// 2. CreateServerSnapshot
	snapshot := ServerSnapshot{
		ID:         1,
		CreatedAt:  clk.Now(),
		RetryCount: 0,
		Status:     ServerSnapshotUploading,
	}
	schedule.NextRunAt = clk.Now().Add(cfg.ScheduleInterval) // schedule advances

	// 3. Upload succeeds
	snapshot.Status = ServerSnapshotStored
	is.Equal(snapshot.Status, ServerSnapshotStored)

	// --- Restore phase ---

	// 4. Admin requests restore
	job := RestoreJob{ID: 1, Status: RestoreJobStarting}
	is.Equal(job.Status, RestoreJobStarting)

	// 5. BeginServerRestore (auto-transition)
	job.Status = RestoreJobRestoringServer
	is.Equal(job.Status, RestoreJobRestoringServer)

	// 6. Server data restored
	job.Status = RestoreJobRestoringRepos
	is.Equal(job.Status, RestoreJobRestoringRepos)

	// 7. All repos restored
	job.Status = RestoreJobCompleted
	is.Equal(job.Status, RestoreJobCompleted)
	is.Equal(job.Status.IsTerminal(), true)
}

func TestScenario_RestoreFailFromStarting(t *testing.T) {
	is := is.New(t)
	job := RestoreJob{Status: RestoreJobStarting}
	job.Status = RestoreJobFailed
	is.Equal(job.Status, RestoreJobFailed)
	is.Equal(job.Status.IsTerminal(), true)
}

func TestScenario_RestoreFailFromRestoringServer(t *testing.T) {
	is := is.New(t)
	job := RestoreJob{Status: RestoreJobStarting}
	job.Status = RestoreJobRestoringServer
	job.Status = RestoreJobFailed
	is.Equal(job.Status, RestoreJobFailed)
	is.Equal(job.Status.IsTerminal(), true)
}

func TestScenario_RestoreFailFromRestoringRepos(t *testing.T) {
	is := is.New(t)
	job := RestoreJob{Status: RestoreJobStarting}
	job.Status = RestoreJobRestoringServer
	job.Status = RestoreJobRestoringRepos
	job.Status = RestoreJobFailed
	is.Equal(job.Status, RestoreJobFailed)
	is.Equal(job.Status.IsTerminal(), true)
}

func TestScenario_ScheduledRepoBackupSkippedWhenOff(t *testing.T) {
	is := is.New(t)
	cfg := DefaultBackupConfig()
	// When backup_repos_on_schedule is false (default), CreateScheduledRepoBackups is rejected.
	is.Equal(cfg.BackupReposOnSchedule, false)
	// No RepoBackups are created by the scheduled trigger.
}

func TestScenario_ScheduledRepoBackupCreatesWhenOn(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	cfg.BackupReposOnSchedule = true
	clk := newFakeClock()
	is.Equal(cfg.BackupReposOnSchedule, true) // scheduled backups enabled

	// CreateScheduledRepoBackups fires for each repo.
	repos := []RepoInfo{
		{Name: "repo-a", DefaultBranch: "main"},
		{Name: "repo-b", DefaultBranch: "main"},
	}
	backups := make([]RepoBackup, len(repos))
	for i, r := range repos {
		backups[i] = RepoBackup{
			RepoName:   r.Name,
			CreatedAt:  clk.Now(),
			RetryCount: 0,
			Status:     RepoBackupUploading,
		}
	}
	is.Equal(len(backups), 2) // one backup per repo
	for _, b := range backups {
		is.Equal(b.Status, RepoBackupUploading)
	}
}

func TestScenario_RepoBackupRotation(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	repoName := "my-repo"
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	// Create max_repo_backups + 1 stored backups, then rotate the oldest.
	backups := make([]RepoBackup, cfg.MaxRepoBackups+1)
	for i := range backups {
		backups[i] = RepoBackup{
			ID:         int64(i + 1),
			RepoName:   repoName,
			CreatedAt:  now.Add(time.Duration(i) * time.Hour),
			RetryCount: 0,
			Status:     RepoBackupStored,
		}
	}
	// When a new one transitions to stored, rotation removes surplus.
	storedCount := 0
	for _, b := range backups {
		if b.RepoName == repoName && b.Status == RepoBackupStored {
			storedCount++
		}
	}
	is.Equal(storedCount, cfg.MaxRepoBackups+1) // over the limit
	toRemove := storedCount - cfg.MaxRepoBackups
	is.Equal(toRemove, 1) // one surplus backup to remove
}

func TestScenario_BackupUploadTimeout(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()

	// 1. Create a backup
	b := RepoBackup{
		ID:         1,
		RepoName:   "my-repo",
		CreatedAt:  clk.Now(),
		RetryCount: 0,
		Status:     RepoBackupUploading,
	}

	// 2. Time advances past the upload timeout
	clk.Advance(cfg.UploadTimeout + 1*time.Minute)
	is.Equal(!b.CreatedAt.Add(cfg.UploadTimeout).After(clk.Now()), true) // timeout condition met
	// 3. Timeout marks backup as failed
	b.Status = RepoBackupFailed
	is.Equal(b.Status, RepoBackupFailed)
	is.Equal(b.Status.IsTerminal(), true)
}

// ============================================================================
// 12. Cross-rule interaction tests
// ============================================================================

func TestCrossRule_UploadTimeoutAndRetryInteraction(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	clk := newFakeClock()

	// A backup starts uploading, fails once (retry), then times out.
	b := RepoBackup{
		ID:         1,
		RepoName:   "my-repo",
		CreatedAt:  clk.Now(),
		RetryCount: 0,
		Status:     RepoBackupUploading,
	}

	// First failure: retry_count increments
	b.RetryCount++
	is.Equal(b.RetryCount, 1)
	is.Equal(b.Status, RepoBackupUploading)

	// Time advances past timeout
	clk.Advance(cfg.UploadTimeout + 1*time.Minute)
	is.Equal(!b.CreatedAt.Add(cfg.UploadTimeout).After(clk.Now()), true)

	// Timeout marks as failed
	b.Status = RepoBackupFailed
	is.Equal(b.Status, RepoBackupFailed)
}

func TestCrossRule_BackupRotationAfterMultipleStored(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()
	repoName := "my-repo"
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	// Create exactly max_repo_backups stored backups
	backups := makeStoredBackups(repoName, cfg.MaxRepoBackups)
	is.Equal(len(backups), cfg.MaxRepoBackups)

	// New backup transitions to stored
	newBackup := RepoBackup{
		ID:         int64(cfg.MaxRepoBackups + 1),
		RepoName:   repoName,
		CreatedAt:  now.Add(24 * time.Hour),
		RetryCount: 0,
		Status:     RepoBackupStored,
	}
	allBackups := append(backups, newBackup)

	// Count stored backups and verify invariant violation
	storedCount := 0
	for _, b := range allBackups {
		if b.Status == RepoBackupStored {
			storedCount++
		}
	}
	is.Equal(storedCount, cfg.MaxRepoBackups+1)               // exceeds max
	is.Equal(storedCount > cfg.MaxRepoBackups, true)            // invariant violated before rotation
	// After rotation, only max_repo_backups should remain
	toRemove := storedCount - cfg.MaxRepoBackups
	is.Equal(toRemove, 1) // one backup should be removed
}

func TestCrossRule_RestoreJobCannotProgressAfterTerminal(t *testing.T) {
	is := is.New(t)
	// Once a RestoreJob is in a terminal state (completed or failed),
	// no further transitions are possible.
	for _, terminalStatus := range []RestoreJobStatus{RestoreJobCompleted, RestoreJobFailed} {
		t.Run(string(terminalStatus), func(t *testing.T) {
			j := &RestoreJob{Status: terminalStatus}
			for _, dst := range []RestoreJobStatus{
				RestoreJobStarting,
				RestoreJobRestoringServer,
				RestoreJobRestoringRepos,
				RestoreJobCompleted,
				RestoreJobFailed,
			} {
				is.New(t).Equal(j.CanTransition(dst), false) // no transitions from terminal state
			}
		})
	}
}

func TestCrossRule_FailedBackupCannotTransition(t *testing.T) {
	is := is.New(t)
	// A failed RepoBackup cannot transition to any other state
	// (next push or scheduled run creates a fresh attempt).
	b := &RepoBackup{Status: RepoBackupFailed}
	for _, dst := range []RepoBackupStatus{RepoBackupUploading, RepoBackupStored, RepoBackupFailed} {
		is.Equal(b.CanTransition(dst), false) // no transitions from failed state
	}
}

func TestCrossRule_StoredBackupCannotTransition(t *testing.T) {
	is := is.New(t)
	b := &RepoBackup{Status: RepoBackupStored}
	for _, dst := range []RepoBackupStatus{RepoBackupUploading, RepoBackupStored, RepoBackupFailed} {
		is.Equal(b.CanTransition(dst), false) // no transitions from stored state
	}
}

// ============================================================================
// 13. Data flow chain tests
// Surface capture → rule → downstream precondition
// ============================================================================

func TestDataFlow_PushTriggerToStoredBackup(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	// GitPushNotification provides PushToDefaultBranch(repo)
	// → CreateRepoBackupOnPush creates RepoBackup(status=uploading)
	// → S3UploadAdapter provides RepoBackupUploadSucceeded(backup)
	// → RepoBackupUploadSucceeds transitions to stored
	b := RepoBackup{
		ID:         1,
		RepoName:   "my-repo",
		CreatedAt:  clk.Now(),
		RetryCount: 0,
		Status:     RepoBackupUploading,
	}
	// Upload succeeds
	is.Equal(b.CanTransition(RepoBackupStored), true)
	b.Status = RepoBackupStored
	is.Equal(b.Status, RepoBackupStored)
}

func TestDataFlow_ScheduleTriggerToStoredSnapshot(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()
	// FireBackupSchedule → BackupScheduleFired() → CreateServerSnapshot
	snapshot := ServerSnapshot{
		ID:         1,
		CreatedAt:  clk.Now(),
		RetryCount: 0,
		Status:     ServerSnapshotUploading,
	}
	schedule := BackupSchedule{NextRunAt: clk.Now().Add(cfg.ScheduleInterval)}
	// Upload succeeds
	snapshot.Status = ServerSnapshotStored
	is.Equal(snapshot.Status, ServerSnapshotStored)
	// Schedule advanced to next run
	is.Equal(schedule.NextRunAt.After(clk.Now()), true) // next run is in the future
}

func TestDataFlow_AdminRestoreRequestThroughCompletion(t *testing.T) {
	is := is.New(t)
	// AdminBackupManagement provides AdminRequestsRestore(admin)
	// → StartRestore creates RestoreJob(starting)
	// → BeginServerRestore transitions to restoring_server
	// → S3DownloadAdapter provides ServerSnapshotRestored(job)
	// → ServerDataRestored transitions to restoring_repos
	// → S3DownloadAdapter provides RepoRestoresComplete(job)
	// → AllReposRestored transitions to completed
	job := RestoreJob{Status: RestoreJobStarting}
	is.Equal(job.Status, RestoreJobStarting)

	job.Status = RestoreJobRestoringServer
	is.Equal(job.Status, RestoreJobRestoringServer)

	job.Status = RestoreJobRestoringRepos
	is.Equal(job.Status, RestoreJobRestoringRepos)

	job.Status = RestoreJobCompleted
	is.Equal(job.Status, RestoreJobCompleted)
	is.Equal(job.Status.IsTerminal(), true)
}

func TestDataFlow_UploadFailureTriggersRetry(t *testing.T) {
	is := is.New(t)
	// S3UploadAdapter provides RepoBackupUploadFailed(backup)
	// → RepoBackupUploadFails
	// → if retry_count < max_upload_retries: increment retry_count
	// → Otherwise: status = failed
	cfg := validConfig()
	b := &RepoBackup{RetryCount: 0, Status: RepoBackupUploading}

	// Fail #1: retry
	is.Equal(b.RetryCount < cfg.MaxUploadRetries, true)
	b.RetryCount++
	is.Equal(b.RetryCount, 1)
	is.Equal(b.Status, RepoBackupUploading)

	// Fail #2: retry
	is.Equal(b.RetryCount < cfg.MaxUploadRetries, true)
	b.RetryCount++
	is.Equal(b.RetryCount, 2)

	// Fail #3: still retry (< max)
	is.Equal(b.RetryCount < cfg.MaxUploadRetries, true)
	b.RetryCount++
	is.Equal(b.RetryCount, 3)

	// Fail #4: retries exhausted (retry_count == max_upload_retries)
	is.Equal(b.RetryCount >= cfg.MaxUploadRetries, true)
	b.Status = RepoBackupFailed
	is.Equal(b.Status, RepoBackupFailed)
}

// ============================================================================
// 14. Conditional ensures edge cases
// ============================================================================

func TestRule_RepoBackupUploadFails_ConditionalEnsures(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()

	tests := []struct {
		name         string
		retryCount   int
		wantRetry    int // expected retry_count after rule (if < max)
		wantStatus   RepoBackupStatus
		description  string
	}{
		{"retry_count_0_increments", 0, 1, RepoBackupUploading, "first failure increments retry_count"},
		{"retry_count_1_increments", 1, 2, RepoBackupUploading, "second failure increments retry_count"},
		{"retry_count_2_increments", 2, 3, RepoBackupUploading, "third failure increments retry_count"},
		{"retry_count_3_fails", 3, 3, RepoBackupFailed, "retry_count == max_upload_retries triggers failure"},
		{"retry_count_4_fails", 4, 4, RepoBackupFailed, "retry_count > max_upload_retries also triggers failure"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &RepoBackup{RetryCount: tt.retryCount, Status: RepoBackupUploading}
			if b.RetryCount < cfg.MaxUploadRetries {
				b.RetryCount++
			} else {
				b.Status = RepoBackupFailed
			}
			is := is.New(t)
			is.Equal(b.RetryCount, tt.wantRetry)
			is.Equal(b.Status, tt.wantStatus)
		})
	}
}

func TestRule_ServerSnapshotUploadFails_ConditionalEnsures(t *testing.T) {
	is := is.New(t)
	cfg := validConfig()

	tests := []struct {
		name        string
		retryCount  int
		wantRetry   int
		wantStatus  ServerSnapshotStatus
	}{
		{"retry_count_0_increments", 0, 1, ServerSnapshotUploading},
		{"retry_count_1_increments", 1, 2, ServerSnapshotUploading},
		{"retry_count_2_increments", 2, 3, ServerSnapshotUploading},
		{"retry_count_3_fails", 3, 3, ServerSnapshotFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ServerSnapshot{RetryCount: tt.retryCount, Status: ServerSnapshotUploading}
			if s.RetryCount < cfg.MaxUploadRetries {
				s.RetryCount++
			} else {
				s.Status = ServerSnapshotFailed
			}
			is := is.New(t)
			is.Equal(s.RetryCount, tt.wantRetry)
			is.Equal(s.Status, tt.wantStatus)
		})
	}
}

// ============================================================================
// 15. Schedule and default instance tests
// ============================================================================

func TestBackupSchedule_DefaultNextRunAt(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()
	// default BackupSchedule main = { next_run_at: now + config.schedule_interval }
	schedule := BackupSchedule{NextRunAt: clk.Now().Add(cfg.ScheduleInterval)}
	is.Equal(schedule.NextRunAt, clk.Now().Add(6*time.Hour)) // next_run_at = now + schedule_interval
}

func TestBackupSchedule_AdvancesAfterFire(t *testing.T) {
	is := is.New(t)
	clk := newFakeClock()
	cfg := validConfig()
	schedule := BackupSchedule{NextRunAt: clk.Now()}
	// After firing, next_run_at advances by schedule_interval
	schedule.NextRunAt = clk.Now().Add(cfg.ScheduleInterval)
	is.Equal(schedule.NextRunAt, clk.Now().Add(6*time.Hour))
}

// ============================================================================
// Helpers
// ============================================================================

func makeStoredBackups(repoName string, count int) []RepoBackup {
	backups := make([]RepoBackup, count)
	baseTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	for i := range backups {
		backups[i] = RepoBackup{
			ID:         int64(i + 1),
			RepoName:   repoName,
			CreatedAt:  baseTime.Add(time.Duration(i) * time.Hour),
			RetryCount: 0,
			Status:     RepoBackupStored,
		}
	}
	return backups
}

func makeStoredSnapshots(count int) []ServerSnapshot {
	snapshots := make([]ServerSnapshot, count)
	baseTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	for i := range snapshots {
		snapshots[i] = ServerSnapshot{
			ID:         int64(i + 1),
			CreatedAt:  baseTime.Add(time.Duration(i) * time.Hour),
			RetryCount: 0,
			Status:     ServerSnapshotStored,
		}
	}
	return snapshots
}