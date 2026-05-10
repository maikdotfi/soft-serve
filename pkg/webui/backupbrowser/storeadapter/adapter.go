// Package storeadapter adapts the backup.BackupStore domain port to the
// backupbrowser.Reader port consumed by pkg/webui.
package storeadapter

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/charmbracelet/soft-serve/pkg/backup"
	"github.com/charmbracelet/soft-serve/pkg/webui/backupbrowser"
)

// Adapter reads backup status through the existing backup store port.
type Adapter struct {
	store backup.BackupStore
}

// New returns an Adapter backed by store. It panics if store is nil.
func New(store backup.BackupStore) *Adapter {
	if store == nil {
		panic("backupbrowser/storeadapter: nil backup store")
	}
	return &Adapter{store: store}
}

// Overview implements backupbrowser.Reader.
func (a *Adapter) Overview(ctx context.Context) (backupbrowser.Overview, error) {
	var overview backupbrowser.Overview

	schedule, err := a.store.GetBackupSchedule(ctx)
	if err != nil {
		if !errors.Is(err, backup.ErrBackupNotFound) {
			return backupbrowser.Overview{}, fmt.Errorf("getting backup schedule: %w", err)
		}
	} else if schedule != nil {
		overview.HasSchedule = true
		overview.NextRunAt = schedule.NextRunAt
	}

	for _, status := range []backup.RepoBackupStatus{
		backup.RepoBackupStored,
		backup.RepoBackupUploading,
		backup.RepoBackupFailed,
	} {
		backups, err := a.store.ListRepoBackupsByStatus(ctx, status)
		if err != nil {
			return backupbrowser.Overview{}, fmt.Errorf("listing repo backups by status %q: %w", status, err)
		}
		for _, b := range backups {
			overview.Records = append(overview.Records, repoRecord(b))
		}
	}

	for _, status := range []backup.ServerSnapshotStatus{
		backup.ServerSnapshotStored,
		backup.ServerSnapshotUploading,
		backup.ServerSnapshotFailed,
	} {
		snapshots, err := a.store.ListServerSnapshotsByStatus(ctx, status)
		if err != nil {
			return backupbrowser.Overview{}, fmt.Errorf("listing server snapshots by status %q: %w", status, err)
		}
		for _, s := range snapshots {
			overview.Records = append(overview.Records, snapshotRecord(s))
		}
	}

	sort.Slice(overview.Records, func(i, j int) bool {
		a, b := overview.Records[i], overview.Records[j]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.ID > b.ID
	})

	for _, record := range overview.Records {
		switch record.Status {
		case backupbrowser.StatusStored:
			if overview.LastStoredAt.IsZero() {
				overview.LastStoredAt = record.CreatedAt
			}
		case backupbrowser.StatusFailed:
			if overview.LastFailedAt.IsZero() {
				overview.LastFailedAt = record.CreatedAt
			}
		}
	}

	return overview, nil
}

func repoRecord(b backup.RepoBackup) backupbrowser.Record {
	return backupbrowser.Record{
		Kind:       backupbrowser.KindRepoBackup,
		ID:         b.ID,
		RepoName:   b.RepoName,
		CreatedAt:  b.CreatedAt,
		RetryCount: b.RetryCount,
		Status:     backupbrowser.Status(b.Status),
	}
}

func snapshotRecord(s backup.ServerSnapshot) backupbrowser.Record {
	return backupbrowser.Record{
		Kind:       backupbrowser.KindServerSnapshot,
		ID:         s.ID,
		CreatedAt:  s.CreatedAt,
		RetryCount: s.RetryCount,
		Status:     backupbrowser.Status(s.Status),
	}
}

var _ backupbrowser.Reader = (*Adapter)(nil)
