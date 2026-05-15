package workitembrowser

import (
	"context"
	"errors"
	"time"
)

var ErrWorkItemNotFound = errors.New("workitembrowser: work item not found")

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

type MessageKind string

const (
	MessageKindCard    MessageKind = "card"
	MessageKindComment MessageKind = "comment"
)

type WorkItemMessage struct {
	ID         int64
	WorkItemID int64
	Kind       MessageKind
	Title      string
	Body       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type WorkItemThread struct {
	Item     WorkItem
	Messages []WorkItemMessage
}

type Reader interface {
	ListByRepo(ctx context.Context, repoName string) ([]WorkItem, error)
	Thread(ctx context.Context, repoName string, id int64) (WorkItemThread, error)
}
