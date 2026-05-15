package workitem

import (
	"context"
	"time"
)

type Store interface {
	Create(ctx context.Context, item WorkItem) (*WorkItem, error)
	Get(ctx context.Context, repoName string, id int64) (*WorkItem, error)
	ListByRepo(ctx context.Context, repoName string) ([]WorkItem, error)
	UpdateLane(ctx context.Context, repoName string, id int64, lane Lane, updatedAt time.Time) (*WorkItem, error)
}

type Clock interface {
	Now() time.Time
}
