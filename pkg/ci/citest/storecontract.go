// Package citest holds shared test helpers for the ci package and
// its adapters. It is imported only from _test.go files; nothing in
// production code depends on it.
package citest

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/ci"
)

// RunStoreContract exercises every method on ci.Store. It is the
// reference contract every Store adapter must satisfy and is what
// keeps the in-memory fake honest against the real SQL adapter.
//
// Pre-condition: the store passed in is empty.
func RunStoreContract(t *testing.T, store ci.Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("RunnerRegistration_NotFoundUntilSaved", func(t *testing.T) {
		if _, err := store.GetRunnerRegistration(ctx, "absent"); !errors.Is(err, ci.ErrRunnerRegistrationNotFound) {
			t.Fatalf("expected ErrRunnerRegistrationNotFound, got %v", err)
		}
	})

	t.Run("RunnerRegistration_RoundTripAndUpsert", func(t *testing.T) {
		registration := ci.RunnerRegistration{Name: "linux-amd64", DispatchURL: "https://r.test/d", SecretToken: "tok-1"}
		if err := store.SaveRunnerRegistration(ctx, registration); err != nil {
			t.Fatalf("save: %v", err)
		}
		got, err := store.GetRunnerRegistration(ctx, "linux-amd64")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.DispatchURL != registration.DispatchURL || got.SecretToken != registration.SecretToken {
			t.Fatalf("round trip = %#v, want %#v", got, registration)
		}
		updated := registration
		updated.DispatchURL = "https://r.test/d2"
		updated.SecretToken = "tok-2"
		if err := store.SaveRunnerRegistration(ctx, updated); err != nil {
			t.Fatalf("upsert save: %v", err)
		}
		got, err = store.GetRunnerRegistration(ctx, "linux-amd64")
		if err != nil {
			t.Fatalf("get after upsert: %v", err)
		}
		if got.DispatchURL != updated.DispatchURL || got.SecretToken != updated.SecretToken {
			t.Fatalf("upsert did not replace: %#v", got)
		}
		if err := store.RemoveRunnerRegistration(ctx, "linux-amd64"); err != nil {
			t.Fatalf("remove: %v", err)
		}
		if _, err := store.GetRunnerRegistration(ctx, "linux-amd64"); !errors.Is(err, ci.ErrRunnerRegistrationNotFound) {
			t.Fatalf("expected not-found after remove, got %v", err)
		}
	})

	t.Run("Workflows_UpsertListAndDeleteExcept", func(t *testing.T) {
		container := "ubuntu:24.04"
		workflowA := ci.Workflow{
			RepoName:  "repo-w",
			Name:      "unit",
			Script:    "go test ./...",
			RunsOn:    "linux-amd64",
			Container: &container,
			Triggers:  map[ci.EventType]bool{ci.EventTypePush: true},
		}
		workflowB := ci.Workflow{
			RepoName: "repo-w",
			Name:     "release",
			Script:   "go build",
			RunsOn:   "linux-arm64",
			Triggers: map[ci.EventType]bool{ci.EventTypeRepository: true},
		}

		for _, workflow := range []ci.Workflow{workflowA, workflowB} {
			if err := store.UpsertWorkflow(ctx, workflow); err != nil {
				t.Fatalf("upsert %q: %v", workflow.Name, err)
			}
		}

		got, err := store.ListWorkflowsByRepo(ctx, "repo-w")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("workflow count = %d, want 2", len(got))
		}
		sort.Slice(got, func(i, j int) bool { return got[i].Name < got[j].Name })
		if got[0].Name != "release" || got[0].RunsOn != "linux-arm64" || got[0].Container != nil {
			t.Fatalf("release workflow = %#v", got[0])
		}
		if got[1].Name != "unit" || got[1].Container == nil || *got[1].Container != container {
			t.Fatalf("unit workflow = %#v", got[1])
		}
		if !got[1].Triggers[ci.EventTypePush] {
			t.Fatalf("unit triggers = %v, want push", got[1].Triggers)
		}

		if err := store.DeleteWorkflowsExcept(ctx, "repo-w", map[string]bool{"unit": true}); err != nil {
			t.Fatalf("delete except: %v", err)
		}
		got, err = store.ListWorkflowsByRepo(ctx, "repo-w")
		if err != nil {
			t.Fatalf("list after delete: %v", err)
		}
		if len(got) != 1 || got[0].Name != "unit" {
			t.Fatalf("workflows after delete = %#v, want [unit]", got)
		}
	})

	t.Run("Run_CreateGetUpdateAndList", func(t *testing.T) {
		now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC).Truncate(time.Second)
		container := "ubuntu:24.04"
		failureReason := ci.FailureReasonRunnerReportedFailure
		run := ci.Run{
			RepoName:         "repo-r",
			WorkflowName:     "unit",
			Script:           "go test ./...",
			RunsOn:           "linux-amd64",
			Container:        &container,
			TriggeredByEvent: ci.EventTypePush,
			Status:           ci.RunPending,
			CreatedAt:        now,
		}
		created, err := store.CreateRun(ctx, run)
		if err != nil {
			t.Fatalf("create run: %v", err)
		}
		if created.ID == 0 {
			t.Fatalf("created run missing ID")
		}
		got, err := store.GetRun(ctx, created.ID)
		if err != nil {
			t.Fatalf("get run: %v", err)
		}
		if got.WorkflowName != "unit" || got.Status != ci.RunPending {
			t.Fatalf("got run = %#v", got)
		}

		started := now.Add(time.Minute).Truncate(time.Second)
		finished := now.Add(2 * time.Minute).Truncate(time.Second)
		got.Status = ci.RunFailed
		got.StartedAt = &started
		got.FinishedAt = &finished
		got.FailureReason = &failureReason
		if err := store.UpdateRun(ctx, *got); err != nil {
			t.Fatalf("update run: %v", err)
		}
		updated, err := store.GetRun(ctx, got.ID)
		if err != nil {
			t.Fatalf("get after update: %v", err)
		}
		if updated.Status != ci.RunFailed {
			t.Fatalf("status = %q, want failed", updated.Status)
		}
		if updated.FailureReason == nil || *updated.FailureReason != failureReason {
			t.Fatalf("failure reason = %v", updated.FailureReason)
		}
		if updated.StartedAt == nil || !updated.StartedAt.Equal(started) {
			t.Fatalf("started at = %v, want %s", updated.StartedAt, started)
		}

		runs, err := store.ListRuns(ctx)
		if err != nil {
			t.Fatalf("list runs: %v", err)
		}
		var found bool
		for _, r := range runs {
			if r.ID == updated.ID {
				found = true
			}
		}
		if !found {
			t.Fatal("ListRuns did not include the updated run")
		}
	})

	t.Run("Run_GetMissingReturnsSentinel", func(t *testing.T) {
		if _, err := store.GetRun(ctx, 999_999); !errors.Is(err, ci.ErrRunNotFound) {
			t.Fatalf("expected ErrRunNotFound, got %v", err)
		}
	})

	t.Run("LogEntries_RoundTripAndCascade", func(t *testing.T) {
		now := time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC).Truncate(time.Second)
		run, err := store.CreateRun(ctx, ci.Run{
			RepoName:         "repo-l",
			WorkflowName:     "unit",
			Script:           "echo hi",
			RunsOn:           "linux-amd64",
			TriggeredByEvent: ci.EventTypePush,
			Status:           ci.RunRunning,
			CreatedAt:        now,
			StartedAt:        &now,
		})
		if err != nil {
			t.Fatalf("create run: %v", err)
		}
		for i, line := range []string{"first", "second", "third"} {
			received := now.Add(time.Duration(i) * time.Second)
			if err := store.CreateLogEntry(ctx, ci.LogEntry{RunID: run.ID, Line: line, ReceivedAt: received}); err != nil {
				t.Fatalf("create log %d: %v", i, err)
			}
		}
		logs, err := store.ListLogEntriesByRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("list logs: %v", err)
		}
		if len(logs) != 3 {
			t.Fatalf("log count = %d, want 3", len(logs))
		}
		if logs[0].Line != "first" || logs[2].Line != "third" {
			t.Fatalf("logs = %#v", logs)
		}
		if err := store.DeleteRun(ctx, run.ID); err != nil {
			t.Fatalf("delete run: %v", err)
		}
		logsAfter, err := store.ListLogEntriesByRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("list logs after delete: %v", err)
		}
		if len(logsAfter) != 0 {
			t.Fatalf("logs not cascaded: %#v", logsAfter)
		}
	})
}
