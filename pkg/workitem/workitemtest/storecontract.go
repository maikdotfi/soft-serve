// Package workitemtest holds shared tests for workitem.Store adapters.
package workitemtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/workitem"
)

func RunStoreContract(t *testing.T, store workitem.Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("ListByRepo_EmptyUntilCreated", func(t *testing.T) {
		items, err := store.ListByRepo(ctx, "empty")
		if err != nil {
			t.Fatalf("ListByRepo: %v", err)
		}
		if len(items) != 0 {
			t.Fatalf("items = %#v, want empty", items)
		}
	})

	t.Run("CreateGetAndList_ScopedByRepo", func(t *testing.T) {
		now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
		alpha, err := store.Create(ctx, workitem.WorkItem{
			RepoName:    "alpha",
			Title:       "Alpha task",
			Description: "visible on alpha",
			Lane:        workitem.LaneBacklog,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		if err != nil {
			t.Fatalf("Create alpha: %v", err)
		}
		if alpha.ID == 0 {
			t.Fatal("Create returned zero ID")
		}
		if _, err := store.Create(ctx, workitem.WorkItem{
			RepoName:  "beta",
			Title:     "Beta task",
			Lane:      workitem.LaneBacklog,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			t.Fatalf("Create beta: %v", err)
		}

		got, err := store.Get(ctx, "alpha", alpha.ID)
		if err != nil {
			t.Fatalf("Get alpha: %v", err)
		}
		if got.Title != "Alpha task" || got.Description != "visible on alpha" {
			t.Fatalf("got = %#v", got)
		}

		items, err := store.ListByRepo(ctx, "alpha")
		if err != nil {
			t.Fatalf("ListByRepo alpha: %v", err)
		}
		if len(items) != 1 || items[0].ID != alpha.ID {
			t.Fatalf("alpha list = %#v, want only alpha item", items)
		}
	})

	t.Run("ListByRepo_OrdersByLaneThenID", func(t *testing.T) {
		now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
		done, err := store.Create(ctx, workitem.WorkItem{RepoName: "ordered", Title: "done", Lane: workitem.LaneDone, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			t.Fatalf("Create done: %v", err)
		}
		backlog, err := store.Create(ctx, workitem.WorkItem{RepoName: "ordered", Title: "backlog", Lane: workitem.LaneBacklog, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			t.Fatalf("Create backlog: %v", err)
		}
		wip, err := store.Create(ctx, workitem.WorkItem{RepoName: "ordered", Title: "wip", Lane: workitem.LaneWIP, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			t.Fatalf("Create wip: %v", err)
		}

		items, err := store.ListByRepo(ctx, "ordered")
		if err != nil {
			t.Fatalf("ListByRepo ordered: %v", err)
		}
		gotIDs := []int64{items[0].ID, items[1].ID, items[2].ID}
		wantIDs := []int64{backlog.ID, wip.ID, done.ID}
		for i := range wantIDs {
			if gotIDs[i] != wantIDs[i] {
				t.Fatalf("order = %v, want %v", gotIDs, wantIDs)
			}
		}
	})

	t.Run("UpdateLane_ReturnsUpdatedItem", func(t *testing.T) {
		createdAt := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
		updatedAt := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
		item, err := store.Create(ctx, workitem.WorkItem{
			RepoName:  "move",
			Title:     "Move me",
			Lane:      workitem.LaneBacklog,
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		moved, err := store.UpdateLane(ctx, "move", item.ID, workitem.LaneWIP, updatedAt)
		if err != nil {
			t.Fatalf("UpdateLane: %v", err)
		}
		if moved.Lane != workitem.LaneWIP {
			t.Fatalf("Lane = %q, want wip", moved.Lane)
		}
		if !moved.UpdatedAt.Equal(updatedAt) {
			t.Fatalf("UpdatedAt = %s, want %s", moved.UpdatedAt, updatedAt)
		}
	})

	t.Run("Messages_AreScopedToWorkItemAndOrderedByID", func(t *testing.T) {
		now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
		alpha, err := store.Create(ctx, workitem.WorkItem{
			RepoName:  "message-alpha",
			Title:     "Alpha task",
			Lane:      workitem.LaneBacklog,
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			t.Fatalf("Create alpha: %v", err)
		}
		beta, err := store.Create(ctx, workitem.WorkItem{
			RepoName:  "message-beta",
			Title:     "Beta task",
			Lane:      workitem.LaneBacklog,
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			t.Fatalf("Create beta: %v", err)
		}

		first, err := store.AddMessage(ctx, workitem.WorkItemMessage{
			RepoName:   "message-alpha",
			WorkItemID: alpha.ID,
			Kind:       workitem.MessageKindComment,
			Body:       "First",
			CreatedAt:  now,
			UpdatedAt:  now,
		})
		if err != nil {
			t.Fatalf("AddMessage first: %v", err)
		}
		second, err := store.AddMessage(ctx, workitem.WorkItemMessage{
			RepoName:   "message-alpha",
			WorkItemID: alpha.ID,
			Kind:       workitem.MessageKindComment,
			Body:       "Second",
			CreatedAt:  now.Add(time.Minute),
			UpdatedAt:  now.Add(time.Minute),
		})
		if err != nil {
			t.Fatalf("AddMessage second: %v", err)
		}
		if _, err := store.AddMessage(ctx, workitem.WorkItemMessage{
			RepoName:   "message-beta",
			WorkItemID: beta.ID,
			Kind:       workitem.MessageKindComment,
			Body:       "Other repo",
			CreatedAt:  now,
			UpdatedAt:  now,
		}); err != nil {
			t.Fatalf("AddMessage beta: %v", err)
		}

		messages, err := store.ListMessages(ctx, "message-alpha", alpha.ID)
		if err != nil {
			t.Fatalf("ListMessages alpha: %v", err)
		}
		if len(messages) != 2 {
			t.Fatalf("messages = %#v, want two alpha messages", messages)
		}
		if messages[0].ID != first.ID || messages[1].ID != second.ID {
			t.Fatalf("message order = %#v, want insertion order", messages)
		}
	})

	t.Run("MissingItem_ReturnsSentinel", func(t *testing.T) {
		if _, err := store.Get(ctx, "missing", 999); !errors.Is(err, workitem.ErrWorkItemNotFound) {
			t.Fatalf("Get error = %v, want ErrWorkItemNotFound", err)
		}
		if _, err := store.UpdateLane(ctx, "missing", 999, workitem.LaneDone, time.Now()); !errors.Is(err, workitem.ErrWorkItemNotFound) {
			t.Fatalf("UpdateLane error = %v, want ErrWorkItemNotFound", err)
		}
		if _, err := store.AddMessage(ctx, workitem.WorkItemMessage{RepoName: "missing", WorkItemID: 999, Kind: workitem.MessageKindComment, Body: "Nope"}); !errors.Is(err, workitem.ErrWorkItemNotFound) {
			t.Fatalf("AddMessage error = %v, want ErrWorkItemNotFound", err)
		}
		if _, err := store.ListMessages(ctx, "missing", 999); !errors.Is(err, workitem.ErrWorkItemNotFound) {
			t.Fatalf("ListMessages error = %v, want ErrWorkItemNotFound", err)
		}
	})
}
