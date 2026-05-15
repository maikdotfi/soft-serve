package workitembrowser

import (
	"context"
	"time"
)

type Lane string

const (
	LaneBacklog Lane = "backlog"
	LaneWIP     Lane = "wip"
	LaneDone    Lane = "done"
)

type WorkItem struct {
	ID          int64
	Title       string
	Description string
	Lane        Lane
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Reader interface {
	ListByRepo(ctx context.Context, repoName string) ([]WorkItem, error)
}
