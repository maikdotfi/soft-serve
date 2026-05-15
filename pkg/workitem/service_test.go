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

func TestService_Thread_IncludesOpeningCardMessage(t *testing.T) {
	createdAt := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 15, 11, 0, 0, 0, time.UTC)
	store := &stubStore{
		items: map[int64]WorkItem{
			7: {
				ID:          7,
				RepoName:    "alpha",
				Title:       "Build task board",
				Description: "API-backed swimlanes",
				Lane:        LaneBacklog,
				CreatedAt:   createdAt,
				UpdatedAt:   updatedAt,
			},
		},
		messages: map[int64][]WorkItemMessage{
			7: {
				{
					ID:         4,
					RepoName:   "alpha",
					WorkItemID: 7,
					Kind:       MessageKindComment,
					Body:       "First follow-up",
					CreatedAt:  updatedAt,
					UpdatedAt:  updatedAt,
				},
			},
		},
	}
	svc := NewService(store, fixedClock{now: updatedAt})

	thread, err := svc.Thread(context.Background(), "alpha", 7)
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}

	if thread.Item.ID != 7 {
		t.Fatalf("thread item ID = %d, want 7", thread.Item.ID)
	}
	if len(thread.Messages) != 2 {
		t.Fatalf("messages = %#v, want opening message plus comment", thread.Messages)
	}
	opening := thread.Messages[0]
	if opening.Kind != MessageKindCard || opening.Title != "Build task board" || opening.Body != "API-backed swimlanes" {
		t.Fatalf("opening message = %#v", opening)
	}
	comment := thread.Messages[1]
	if comment.Kind != MessageKindComment || comment.Body != "First follow-up" {
		t.Fatalf("comment message = %#v", comment)
	}
}

func TestService_AddMessage_TrimsAndTimestampsComment(t *testing.T) {
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	store := &stubStore{
		items: map[int64]WorkItem{
			7: {ID: 7, RepoName: "alpha", Title: "Build task board", Lane: LaneBacklog, CreatedAt: now, UpdatedAt: now},
		},
	}
	svc := NewService(store, fixedClock{now: now})

	message, err := svc.AddMessage(context.Background(), "alpha", 7, "  Ship it  ")
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	if message.WorkItemID != 7 || message.RepoName != "alpha" {
		t.Fatalf("message scope = %#v", message)
	}
	if message.Kind != MessageKindComment {
		t.Fatalf("message kind = %q, want comment", message.Kind)
	}
	if message.Body != "Ship it" {
		t.Fatalf("message body = %q, want trimmed body", message.Body)
	}
	if !message.CreatedAt.Equal(now) || !message.UpdatedAt.Equal(now) {
		t.Fatalf("timestamps = %s/%s, want %s", message.CreatedAt, message.UpdatedAt, now)
	}
}

func TestService_AddMessage_RejectsBlankBody(t *testing.T) {
	svc := NewService(&stubStore{}, fixedClock{now: time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)})

	if _, err := svc.AddMessage(context.Background(), "alpha", 7, "  "); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("AddMessage error = %v, want ErrInvalidMessage", err)
	}
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type stubStore struct {
	items       map[int64]WorkItem
	messages    map[int64][]WorkItemMessage
	next        int64
	nextMessage int64
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

func (s *stubStore) AddMessage(_ context.Context, message WorkItemMessage) (*WorkItemMessage, error) {
	item, ok := s.items[message.WorkItemID]
	if !ok || item.RepoName != message.RepoName {
		return nil, ErrWorkItemNotFound
	}
	s.nextMessage++
	message.ID = s.nextMessage
	if s.messages == nil {
		s.messages = map[int64][]WorkItemMessage{}
	}
	s.messages[message.WorkItemID] = append(s.messages[message.WorkItemID], message)
	out := message
	return &out, nil
}

func (s *stubStore) ListMessages(_ context.Context, repoName string, workItemID int64) ([]WorkItemMessage, error) {
	item, ok := s.items[workItemID]
	if !ok || item.RepoName != repoName {
		return nil, ErrWorkItemNotFound
	}
	return append([]WorkItemMessage(nil), s.messages[workItemID]...), nil
}
