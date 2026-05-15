package workitem

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestService_Create_DefaultsToBacklogAndTimestamps(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	store := &stubStore{}
	svc := NewService(store, fixedClock{now: now})

	item, err := svc.Create(ctx, "alpha", "Ship board", "per-repo task board")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if item.RepoName != "alpha" {
		t.Fatalf("RepoName = %q, want alpha", item.RepoName)
	}
	if item.Title != "Ship board" {
		t.Fatalf("Title = %q, want Ship board", item.Title)
	}
	if item.Description != "per-repo task board" {
		t.Fatalf("Description = %q, want per-repo task board", item.Description)
	}
	if item.Lane != LaneBacklog {
		t.Fatalf("Lane = %q, want backlog", item.Lane)
	}
	if !item.CreatedAt.Equal(now) || !item.UpdatedAt.Equal(now) {
		t.Fatalf("timestamps = %s/%s, want %s", item.CreatedAt, item.UpdatedAt, now)
	}
}

func TestService_Create_RejectsBlankTitle(t *testing.T) {
	svc := NewService(&stubStore{}, fixedClock{now: time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)})

	if _, err := svc.Create(context.Background(), "alpha", "  ", ""); !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("Create error = %v, want ErrInvalidTitle", err)
	}
}

func TestService_Move_UpdatesLaneAndTimestamp(t *testing.T) {
	ctx := context.Background()
	createdAt := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	movedAt := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	store := &stubStore{
		items: map[int64]WorkItem{
			7: {
				ID:        7,
				RepoName:  "alpha",
				Title:     "Wire API",
				Lane:      LaneBacklog,
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
			},
		},
	}
	svc := NewService(store, fixedClock{now: movedAt})

	item, err := svc.Move(ctx, "alpha", 7, LaneWIP)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}

	if item.Lane != LaneWIP {
		t.Fatalf("Lane = %q, want wip", item.Lane)
	}
	if !item.UpdatedAt.Equal(movedAt) {
		t.Fatalf("UpdatedAt = %s, want %s", item.UpdatedAt, movedAt)
	}
	if !item.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %s, want unchanged %s", item.CreatedAt, createdAt)
	}
}

func TestService_Move_RejectsInvalidLane(t *testing.T) {
	svc := NewService(&stubStore{}, fixedClock{now: time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)})

	if _, err := svc.Move(context.Background(), "alpha", 7, Lane("review")); !errors.Is(err, ErrInvalidLane) {
		t.Fatalf("Move error = %v, want ErrInvalidLane", err)
	}
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type stubStore struct {
	items map[int64]WorkItem
	next  int64
}

func (s *stubStore) Create(_ context.Context, item WorkItem) (*WorkItem, error) {
	s.next++
	item.ID = s.next
	if s.items == nil {
		s.items = map[int64]WorkItem{}
	}
	s.items[item.ID] = item
	out := item
	return &out, nil
}

func (s *stubStore) Get(_ context.Context, repoName string, id int64) (*WorkItem, error) {
	item, ok := s.items[id]
	if !ok || item.RepoName != repoName {
		return nil, ErrWorkItemNotFound
	}
	out := item
	return &out, nil
}

func (s *stubStore) ListByRepo(_ context.Context, repoName string) ([]WorkItem, error) {
	var out []WorkItem
	for _, item := range s.items {
		if item.RepoName == repoName {
			out = append(out, item)
		}
	}
	return out, nil
}

func (s *stubStore) UpdateLane(_ context.Context, repoName string, id int64, lane Lane, updatedAt time.Time) (*WorkItem, error) {
	item, ok := s.items[id]
	if !ok || item.RepoName != repoName {
		return nil, ErrWorkItemNotFound
	}
	item.Lane = lane
	item.UpdatedAt = updatedAt
	s.items[id] = item
	out := item
	return &out, nil
}
