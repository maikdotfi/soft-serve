package storeadapter_test

import (
	"context"
	"testing"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/backup"
	backupfake "github.com/charmbracelet/soft-serve/pkg/backup/adapters/fake"
	"github.com/charmbracelet/soft-serve/pkg/webui/backupbrowser"
	"github.com/charmbracelet/soft-serve/pkg/webui/backupbrowser/storeadapter"
)

func TestAdapter_OverviewListsRecentBackupAttempts(t *testing.T) {
	ctx := context.Background()
	store := backupfake.NewFakeBackupStore()
	nextRunAt := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	if err := store.SetBackupScheduleNextRunAt(ctx, nextRunAt); err != nil {
		t.Fatalf("SetBackupScheduleNextRunAt: %v", err)
	}

	storedSnapshot, err := store.CreateServerSnapshot(ctx, time.Date(2026, 4, 2, 9, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateServerSnapshot: %v", err)
	}
	if err := store.UpdateServerSnapshotStatus(ctx, storedSnapshot.ID, backup.ServerSnapshotStored, 0); err != nil {
		t.Fatalf("UpdateServerSnapshotStatus: %v", err)
	}
	failedRepo, err := store.CreateRepoBackup(ctx, "alpha", time.Date(2026, 4, 2, 11, 15, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateRepoBackup failed: %v", err)
	}
	if err := store.UpdateRepoBackupStatus(ctx, failedRepo.ID, backup.RepoBackupFailed, 3); err != nil {
		t.Fatalf("UpdateRepoBackupStatus: %v", err)
	}
	uploadingRepo, err := store.CreateRepoBackup(ctx, "beta", time.Date(2026, 4, 2, 10, 45, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateRepoBackup uploading: %v", err)
	}

	reader := storeadapter.New(store)
	overview, err := reader.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	if !overview.HasSchedule {
		t.Fatal("Overview HasSchedule = false, want true")
	}
	if !overview.NextRunAt.Equal(nextRunAt) {
		t.Fatalf("NextRunAt = %v, want %v", overview.NextRunAt, nextRunAt)
	}
	if !overview.LastStoredAt.Equal(storedSnapshot.CreatedAt) {
		t.Fatalf("LastStoredAt = %v, want %v", overview.LastStoredAt, storedSnapshot.CreatedAt)
	}
	if !overview.LastFailedAt.Equal(failedRepo.CreatedAt) {
		t.Fatalf("LastFailedAt = %v, want %v", overview.LastFailedAt, failedRepo.CreatedAt)
	}
	if len(overview.Records) != 3 {
		t.Fatalf("records len = %d, want 3: %#v", len(overview.Records), overview.Records)
	}
	assertRecord(t, overview.Records[0], backupbrowser.Record{
		Kind:       backupbrowser.KindRepoBackup,
		ID:         failedRepo.ID,
		RepoName:   "alpha",
		CreatedAt:  failedRepo.CreatedAt,
		RetryCount: 3,
		Status:     backupbrowser.StatusFailed,
	})
	assertRecord(t, overview.Records[1], backupbrowser.Record{
		Kind:      backupbrowser.KindRepoBackup,
		ID:        uploadingRepo.ID,
		RepoName:  "beta",
		CreatedAt: uploadingRepo.CreatedAt,
		Status:    backupbrowser.StatusUploading,
	})
	assertRecord(t, overview.Records[2], backupbrowser.Record{
		Kind:      backupbrowser.KindServerSnapshot,
		ID:        storedSnapshot.ID,
		CreatedAt: storedSnapshot.CreatedAt,
		Status:    backupbrowser.StatusStored,
	})
}

func TestAdapter_OverviewAllowsMissingSchedule(t *testing.T) {
	reader := storeadapter.New(backupfake.NewFakeBackupStore())
	overview, err := reader.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if overview.HasSchedule {
		t.Fatal("Overview HasSchedule = true, want false")
	}
}

func assertRecord(t *testing.T, got, want backupbrowser.Record) {
	t.Helper()
	if got.Kind != want.Kind ||
		got.ID != want.ID ||
		got.RepoName != want.RepoName ||
		!got.CreatedAt.Equal(want.CreatedAt) ||
		got.RetryCount != want.RetryCount ||
		got.Status != want.Status {
		t.Fatalf("record = %#v, want %#v", got, want)
	}
}
