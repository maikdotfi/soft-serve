// Package backupbrowser defines the read-only backup status port consumed by
// pkg/webui.
//
// Like repobrowser, this package expresses what the UI needs in UI/domain
// terms. Concrete adapters translate existing backup stores or services onto
// this port at the composition root.
package backupbrowser

import (
	"context"
	"time"
)

// Status is the shared status vocabulary for backup attempts.
type Status string

const (
	StatusUploading Status = "uploading"
	StatusStored    Status = "stored"
	StatusFailed    Status = "failed"
)

// Kind identifies the concrete backup attempt represented by a Record.
type Kind string

const (
	KindRepoBackup     Kind = "repo backup"
	KindServerSnapshot Kind = "server snapshot"
)

// Record is a single backup attempt shown in the UI.
type Record struct {
	Kind       Kind
	ID         int64
	RepoName   string
	CreatedAt  time.Time
	RetryCount int
	Status     Status
}

// Overview is the backup status summary rendered by the web UI.
type Overview struct {
	HasSchedule bool
	NextRunAt   time.Time

	LastStoredAt time.Time
	LastFailedAt time.Time

	Records []Record
}

// Reader is the read-only port the web UI consumes for backup status.
type Reader interface {
	Overview(ctx context.Context) (Overview, error)
}
