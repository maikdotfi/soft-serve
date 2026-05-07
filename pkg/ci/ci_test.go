package ci_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/ci"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/fake"
	"github.com/matryer/is"
)

// newFixture returns a fully wired test fixture.
func newFixture(t *testing.T) *fake.Setup {
	t.Helper()
	return fake.NewTestSetup()
}

// --- Type and Enum Tests ---

func TestEnum_EventType_ValidValues(t *testing.T) {
	is := is.New(t)
	for _, et := range []ci.EventType{
		ci.EventTypePush,
		ci.EventTypeBranchTagCreate,
		ci.EventTypeBranchTagDelete,
		ci.EventTypeCollaborator,
		ci.EventTypeRepository,
		ci.EventTypeRepositoryVisibilityChange,
	} {
		is.True(ci.ValidEventTypes[et])
	}
}

func TestEnum_FailureReason_ValidValues(t *testing.T) {
	is := is.New(t)
	for _, fr := range []ci.FailureReason{
		ci.FailureReasonDispatchAckFailed,
		ci.FailureReasonPickupTimeout,
		ci.FailureReasonRunnerReportedFailure,
		ci.FailureReasonUnknownRunner,
	} {
		is.True(ci.ValidFailureReasons[fr])
	}
}

func TestEnum_RunStatus_ValidValues(t *testing.T) {
	is := is.New(t)
	for _, s := range []ci.RunStatus{
		ci.RunStatusPending,
		ci.RunStatusDispatched,
		ci.RunStatusRunning,
		ci.RunStatusSucceeded,
		ci.RunStatusFailed,
		ci.RunStatusCanceled,
	} {
		is.True(ci.ValidRunStatuses[s])
	}
}

// --- Transition Graph Tests ---

func TestTransitionGraph_ValidTransitions(t *testing.T) {
	tests := []struct {
		name string
		from ci.RunStatus
		to   ci.RunStatus
		ok   bool
	}{
		// From pending
		{"pending to dispatched", ci.RunStatusPending, ci.RunStatusDispatched, true},
		{"pending to failed", ci.RunStatusPending, ci.RunStatusFailed, true},
		{"pending to canceled", ci.RunStatusPending, ci.RunStatusCanceled, true},
		{"pending to running (invalid)", ci.RunStatusPending, ci.RunStatusRunning, false},
		{"pending to succeeded (invalid)", ci.RunStatusPending, ci.RunStatusSucceeded, false},

		// From dispatched
		{"dispatched to running", ci.RunStatusDispatched, ci.RunStatusRunning, true},
		{"dispatched to failed", ci.RunStatusDispatched, ci.RunStatusFailed, true},
		{"dispatched to canceled", ci.RunStatusDispatched, ci.RunStatusCanceled, true},
		{"dispatched to pending (invalid)", ci.RunStatusDispatched, ci.RunStatusPending, false},
		{"dispatched to succeeded (invalid)", ci.RunStatusDispatched, ci.RunStatusSucceeded, false},

		// From running
		{"running to succeeded", ci.RunStatusRunning, ci.RunStatusSucceeded, true},
		{"running to failed", ci.RunStatusRunning, ci.RunStatusFailed, true},
		{"running to canceled", ci.RunStatusRunning, ci.RunStatusCanceled, true},
		{"running to pending (invalid)", ci.RunStatusRunning, ci.RunStatusPending, false},
		{"running to dispatched (invalid)", ci.RunStatusRunning, ci.RunStatusDispatched, false},

		// From terminal states
		{"succeeded to anything (invalid)", ci.RunStatusSucceeded, ci.RunStatusFailed, false},
		{"failed to anything (invalid)", ci.RunStatusFailed, ci.RunStatusSucceeded, false},
		{"canceled to anything (invalid)", ci.RunStatusCanceled, ci.RunStatusSucceeded, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			is.Equal(ci.CanTransition(tt.from, tt.to), tt.ok)
		})
	}
}

func TestTransitionGraph_TerminalStates(t *testing.T) {
	is := is.New(t)
	is.True(ci.IsTerminal(ci.RunStatusSucceeded))
	is.True(ci.IsTerminal(ci.RunStatusFailed))
	is.True(ci.IsTerminal(ci.RunStatusCanceled))

	is.True(!ci.IsTerminal(ci.RunStatusPending))
	is.True(!ci.IsTerminal(ci.RunStatusDispatched))
	is.True(!ci.IsTerminal(ci.RunStatusRunning))
}

// --- State-Dependent Field Tests ---

func TestStateDependentFields_StartedAt(t *testing.T) {
	is := is.New(t)
	// started_at must be present for running and post-running states
	for _, s := range []ci.RunStatus{ci.RunStatusRunning, ci.RunStatusSucceeded, ci.RunStatusFailed, ci.RunStatusCanceled} {
		is.True(ci.HasStartedAt(s))
	}
	// started_at must NOT be present for pre-running states
	for _, s := range []ci.RunStatus{ci.RunStatusPending, ci.RunStatusDispatched} {
		is.True(!ci.HasStartedAt(s))
	}
}

func TestStateDependentFields_FinishedAt(t *testing.T) {
	is := is.New(t)
	// finished_at must be present for terminal states
	for _, s := range []ci.RunStatus{ci.RunStatusSucceeded, ci.RunStatusFailed, ci.RunStatusCanceled} {
		is.True(ci.HasFinishedAt(s))
	}
	// finished_at must NOT be present for non-terminal states
	for _, s := range []ci.RunStatus{ci.RunStatusPending, ci.RunStatusDispatched, ci.RunStatusRunning} {
		is.True(!ci.HasFinishedAt(s))
	}
}

func TestStateDependentFields_FailureReason(t *testing.T) {
	is := is.New(t)
	// failure_reason must be present only for failed
	is.True(ci.HasFailureReason(ci.RunStatusFailed))
	for _, s := range []ci.RunStatus{ci.RunStatusPending, ci.RunStatusDispatched, ci.RunStatusRunning, ci.RunStatusSucceeded, ci.RunStatusCanceled} {
		is.True(!ci.HasFailureReason(s))
	}
}

// --- Config Tests ---

func TestConfig_DefaultValues(t *testing.T) {
	is := is.New(t)
	cfg := ci.DefaultCIConfig()
	is.Equal(cfg.PickupTimeout, 1*time.Hour)
	is.Equal(cfg.RunRetention, 7*24*time.Hour)
}

// --- Runner Registration Tests ---

func TestRunnerRegistration_Create(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	runner, err := fx.Service.RegisterRunner(ctx, "docker-runner", "https://runner.example.com/dispatch")
	is.NoErr(err)
	is.Equal(runner.Name, "docker-runner")
	is.Equal(runner.DispatchURL, "https://runner.example.com/dispatch")
	is.True(runner.SecretToken != "") // token is generated
	is.True(runner.ID > 0)

	// Verify it can be looked up
	found, err := fx.Store.GetRunnerRegistrationByName(ctx, "docker-runner")
	is.NoErr(err)
	is.Equal(found.ID, runner.ID)
}

func TestRunnerRegistration_List(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	_, err := fx.Service.RegisterRunner(ctx, "runner-a", "https://a.example.com")
	is.NoErr(err)
	_, err = fx.Service.RegisterRunner(ctx, "runner-b", "https://b.example.com")
	is.NoErr(err)

	all, err := fx.Service.ListRunners(ctx)
	is.NoErr(err)
	is.Equal(len(all), 2)
}

func TestRunnerRegistration_Remove(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	_, err := fx.Service.RegisterRunner(ctx, "ephemeral", "https://ep.example.com")
	is.NoErr(err)

	err = fx.Service.RemoveRunner(ctx, "ephemeral")
	is.NoErr(err)

	_, err = fx.Store.GetRunnerRegistrationByName(ctx, "ephemeral")
	is.True(err != nil) // not found after removal
}

func TestRunnerRegistration_RemoveNonexistent(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	err := fx.Service.RemoveRunner(ctx, "nonexistent")
	is.True(errors.Is(err, ci.ErrRunnerRegistrationNotFound))
}

// --- Workflow Sync Tests (rule WorkflowsSyncedOnPush) ---

func TestWorkflow_SyncOnPush_CreatesNewWorkflows(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	err := fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
		{Name: "lint", Script: "golangci-lint run", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	wfs, err := fx.Service.ListWorkflows(ctx, "my-repo")
	is.NoErr(err)
	is.Equal(len(wfs), 2)

	names := make(map[string]bool)
	for _, w := range wfs {
		names[w.Name] = true
	}
	is.True(names["test"])
	is.True(names["lint"])
}

func TestWorkflow_SyncOnPush_RemovesStaleWorkflows(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	// Create initial set
	err := fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
		{Name: "lint", Script: "golangci-lint run", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	// Push again with only one workflow (test was deleted)
	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "lint", Script: "golangci-lint run", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	wfs, err := fx.Service.ListWorkflows(ctx, "my-repo")
	is.NoErr(err)
	is.Equal(len(wfs), 1)
	is.Equal(wfs[0].Name, "lint")
}

func TestWorkflow_SyncOnPush_UpdatesExistingWorkflow(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	err := fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "runner-a", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	// Update the workflow (different runner, different script)
	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test -v ./...", RunsOn: "runner-b", Triggers: []ci.EventType{ci.EventTypePush, ci.EventTypeBranchTagCreate}},
	})
	is.NoErr(err)

	wfs, err := fx.Service.ListWorkflows(ctx, "my-repo")
	is.NoErr(err)
	is.Equal(len(wfs), 1)
	is.Equal(wfs[0].Script, "go test -v ./...")
	is.Equal(wfs[0].RunsOn, "runner-b")
	is.Equal(len(wfs[0].Triggers), 2)
}

func TestWorkflow_SyncOnPush_EmptySet_RemovesAll(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	err := fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	// Push with empty workflows
	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", nil)
	is.NoErr(err)

	wfs, err := fx.Service.ListWorkflows(ctx, "my-repo")
	is.NoErr(err)
	is.Equal(len(wfs), 0)
}

func TestWorkflow_OptionalContainer(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	containerImg := "golang:1.25"
	err := fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "docker-runner", Container: &containerImg, Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	wfs, err := fx.Service.ListWorkflows(ctx, "my-repo")
	is.NoErr(err)
	is.Equal(len(wfs), 1)
	is.True(wfs[0].Container != nil)
	is.Equal(*wfs[0].Container, "golang:1.25")
}

// --- Run Creation Tests (rule CreateRunsOnEvent) ---

func TestRun_CreateOnEvent_CreatesRunsForMatchingWorkflows(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	// Setup: register runner and sync workflows
	_, err := fx.Service.RegisterRunner(ctx, "docker-runner", "https://runner.example.com/dispatch")
	is.NoErr(err)

	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
		{Name: "lint", Script: "golangci-lint run", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	// Fire push event
	runs, err := fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypePush)
	is.NoErr(err)
	is.Equal(len(runs), 2)

	for _, r := range runs {
		is.Equal(r.RepoName, "my-repo")
		is.Equal(r.Status, ci.RunStatusPending)
		is.Equal(r.TriggeredByEvent, ci.EventTypePush)
		is.True(r.CreatedAt.Equal(fx.Clock.Now()))
	}
}

func TestRun_CreateOnEvent_OnlyMatchingTriggers(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	_, err := fx.Service.RegisterRunner(ctx, "docker-runner", "https://runner.example.com/dispatch")
	is.NoErr(err)

	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "push-only", Script: "echo push", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
		{Name: "tag-only", Script: "echo tag", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypeBranchTagCreate}},
	})
	is.NoErr(err)

	// Fire branch_tag_create event — only tag-only should fire
	runs, err := fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypeBranchTagCreate)
	is.NoErr(err)
	is.Equal(len(runs), 1)
	is.Equal(runs[0].WorkflowName, "tag-only")
}

func TestRun_CreateOnEvent_NoMatchingWorkflows_CreatesNothing(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	_, err := fx.Service.RegisterRunner(ctx, "docker-runner", "https://runner.example.com/dispatch")
	is.NoErr(err)

	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "push-only", Script: "echo push", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	// Fire an unmatched event
	runs, err := fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypeCollaborator)
	is.NoErr(err)
	is.Equal(len(runs), 0)
}

func TestRun_SnapshotsWorkflowFields(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	_, err := fx.Service.RegisterRunner(ctx, "docker-runner", "https://runner.example.com/dispatch")
	is.NoErr(err)

	containerImg := "golang:1.25"
	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "docker-runner", Container: &containerImg, Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	// Create run (snapshots workflow)
	runs, err := fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypePush)
	is.NoErr(err)
	is.Equal(len(runs), 1)
	is.Equal(runs[0].Script, "go test ./...")
	is.Equal(runs[0].RunsOn, "docker-runner")
	is.Equal(runs[0].WorkflowName, "test")
	is.True(runs[0].Container != nil)
	is.Equal(*runs[0].Container, "golang:1.25")

	// Now modify the workflow — the run should NOT be affected
	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test -race ./...", RunsOn: "runner-b", Container: nil, Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	// Fetch the run again — fields should be unchanged (snapshot preserved)
	run, err := fx.Service.GetRun(ctx, runs[0].ID)
	is.NoErr(err)
	is.Equal(run.Script, "go test ./...")         // original script
	is.Equal(run.RunsOn, "docker-runner")          // original runner
	is.True(run.Container != nil)
	is.Equal(*run.Container, "golang:1.25")        // original container
}

// --- Dispatch Tests (rules DispatchRun, UnknownRunner, DispatchAckFailed) ---

func TestDispatch_Success_PendingToDispatched(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	_, err := fx.Service.RegisterRunner(ctx, "docker-runner", "https://runner.example.com/dispatch")
	is.NoErr(err)

	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	runs, err := fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypePush)
	is.NoErr(err)
	is.Equal(len(runs), 1)

	// Dispatch the run
	dispatched, err := fx.Service.DispatchRun(ctx, runs[0].ID)
	is.NoErr(err)
	is.Equal(dispatched.Status, ci.RunStatusDispatched)
	is.True(fx.Dispatch.WasDispatched(runs[0].ID))
}

func TestDispatch_UnknownRunner_FailsImmediately(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	// Create a run referencing a non-existent runner
	err := fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "nonexistent-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	runs, err := fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypePush)
	is.NoErr(err)
	is.Equal(len(runs), 1)

	// Dispatch should fail with unknown_runner
	result, err := fx.Service.DispatchRun(ctx, runs[0].ID)
	is.NoErr(err) // no error from DispatchRun — it handles this gracefully
	is.Equal(result.Status, ci.RunStatusFailed)
	is.True(result.FailureReason != nil)
	is.Equal(*result.FailureReason, ci.FailureReasonUnknownRunner)
	is.True(result.FinishedAt != nil)
	is.True(!fx.Dispatch.WasDispatched(runs[0].ID)) // never sent to runner
}

func TestDispatch_AckFailed_FailsImmediately(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	_, err := fx.Service.RegisterRunner(ctx, "docker-runner", "https://runner.example.com/dispatch")
	is.NoErr(err)

	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	runs, err := fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypePush)
	is.NoErr(err)

	// Simulate dispatch failure
	fx.Dispatch.DispatchErr = errors.New("connection refused")

	result, err := fx.Service.DispatchRun(ctx, runs[0].ID)
	is.NoErr(err) // no error from DispatchRun
	is.Equal(result.Status, ci.RunStatusFailed)
	is.True(result.FailureReason != nil)
	is.Equal(*result.FailureReason, ci.FailureReasonDispatchAckFailed)
	is.True(result.FinishedAt != nil)
}

func TestDispatch_HandleDispatchFailed_SeparateMethod(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	_, err := fx.Service.RegisterRunner(ctx, "docker-runner", "https://runner.example.com/dispatch")
	is.NoErr(err)

	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	runs, err := fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypePush)
	is.NoErr(err)

	// Use the separate HandleDispatchFailed method
	result, err := fx.Service.HandleDispatchFailed(ctx, runs[0].ID)
	is.NoErr(err)
	is.Equal(result.Status, ci.RunStatusFailed)
	is.Equal(*result.FailureReason, ci.FailureReasonDispatchAckFailed)
	is.True(result.FinishedAt != nil)
}

func TestDispatch_HandleDispatchFailed_WrongStatus(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	_, err := fx.Service.RegisterRunner(ctx, "docker-runner", "https://runner.example.com/dispatch")
	is.NoErr(err)

	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	runs, err := fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypePush)
	is.NoErr(err)

	// First, dispatch successfully (pending -> dispatched)
	_, err = fx.Service.DispatchRun(ctx, runs[0].ID)
	is.NoErr(err)

	// Now try HandleDispatchFailed on a dispatched run — should reject
	_, err = fx.Service.HandleDispatchFailed(ctx, runs[0].ID)
	is.True(errors.Is(err, ci.ErrInvalidTransition))
}

// --- Runner Callback Tests (rules RunStarted, RunSucceeded, RunFailed) ---

func TestRunnerCallback_Started_DispatchedToRunning(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareDispatchedRun(t, fx, ctx)

	result, err := fx.Service.HandleRunnerStarted(ctx, run.ID)
	is.NoErr(err)
	is.Equal(result.Status, ci.RunStatusRunning)
	is.True(result.StartedAt != nil)
	is.True(result.StartedAt.Equal(fx.Clock.Now()))
}

func TestRunnerCallback_Started_WrongStatus(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := preparePendingRun(t, fx, ctx)

	_, err := fx.Service.HandleRunnerStarted(ctx, run.ID)
	is.True(errors.Is(err, ci.ErrInvalidTransition))
}

func TestRunnerCallback_Succeeded_ExitCodeZero(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareRunningRun(t, fx, ctx)

	result, err := fx.Service.HandleRunnerCompletion(ctx, run.ID, 0)
	is.NoErr(err)
	is.Equal(result.Status, ci.RunStatusSucceeded)
	is.True(result.FinishedAt != nil)
	is.True(result.FinishedAt.Equal(fx.Clock.Now()))
	is.True(result.FailureReason == nil) // success has no failure reason
}

func TestRunnerCallback_Failed_NonzeroExitCode(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareRunningRun(t, fx, ctx)

	result, err := fx.Service.HandleRunnerCompletion(ctx, run.ID, 1)
	is.NoErr(err)
	is.Equal(result.Status, ci.RunStatusFailed)
	is.True(result.FinishedAt != nil)
	is.True(result.FailureReason != nil)
	is.Equal(*result.FailureReason, ci.FailureReasonRunnerReportedFailure)
}

func TestRunnerCallback_Completion_WrongStatus(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareDispatchedRun(t, fx, ctx)

	_, err := fx.Service.HandleRunnerCompletion(ctx, run.ID, 0)
	is.True(errors.Is(err, ci.ErrInvalidTransition))
}

// --- Log Ingestion Tests (rule IngestLogLine) ---

func TestLogIngestion_AddsLogEntryForRunningRun(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareRunningRun(t, fx, ctx)

	entry, err := fx.Service.IngestLogLine(ctx, run.ID, "Starting build...")
	is.NoErr(err)
	is.True(entry.ID > 0)
	is.Equal(entry.RunID, run.ID)
	is.Equal(entry.Line, "Starting build...")
	is.True(entry.ReceivedAt.Equal(fx.Clock.Now()))
}

func TestLogIngestion_MultipleLines(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareRunningRun(t, fx, ctx)

	for i, line := range []string{"line 1", "line 2", "line 3"} {
		_, err := fx.Service.IngestLogLine(ctx, run.ID, line)
		is.NoErr(err)

		entries, err := fx.Service.ListLogEntries(ctx, run.ID)
		is.NoErr(err)
		is.Equal(len(entries), i+1)
	}
}

func TestLogIngestion_RejectsNonRunningRun(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareDispatchedRun(t, fx, ctx)

	_, err := fx.Service.IngestLogLine(ctx, run.ID, "some log")
	is.True(errors.Is(err, ci.ErrInvalidTransition))
}

// --- Cancellation Tests (rules UserCancelsPendingRun, CancelAckedFromDispatched, CancelAckedFromRunning) ---

func TestCancel_FromPending_ImmediateCancel(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := preparePendingRun(t, fx, ctx)

	result, err := fx.Service.CancelRun(ctx, run.ID)
	is.NoErr(err)
	is.Equal(result.Status, ci.RunStatusCanceled)
	is.True(result.FinishedAt != nil)
	is.True(!fx.Dispatch.WasCanceled(run.ID)) // no cancel webhook sent for pending runs
}

func TestCancel_FromDispatched_SendsCancelWebhook(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareDispatchedRun(t, fx, ctx)

	result, err := fx.Service.CancelRun(ctx, run.ID)
	is.NoErr(err)
	is.Equal(result.Status, ci.RunStatusCanceled)
	is.True(result.FinishedAt != nil)
	is.True(fx.Dispatch.WasCanceled(run.ID)) // cancel webhook was sent
}

func TestCancel_FromRunning_SendsCancelWebhook(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareRunningRun(t, fx, ctx)

	result, err := fx.Service.CancelRun(ctx, run.ID)
	is.NoErr(err)
	is.Equal(result.Status, ci.RunStatusCanceled)
	is.True(result.FinishedAt != nil)
	is.True(fx.Dispatch.WasCanceled(run.ID))
}

func TestCancel_CancelWebhookFails_RunStaysInCurrentState(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareDispatchedRun(t, fx, ctx)

	fx.Dispatch.CancelErr = errors.New("runner unreachable")

	_, err := fx.Service.CancelRun(ctx, run.ID)
	is.True(err != nil) // cancel fails

	// Run should still be in dispatched state
	current, err := fx.Service.GetRun(ctx, run.ID)
	is.NoErr(err)
	is.Equal(current.Status, ci.RunStatusDispatched)
}

func TestCancel_TerminalRun_Rejected(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareRunningRun(t, fx, ctx)

	// Complete the run first
	_, err := fx.Service.HandleRunnerCompletion(ctx, run.ID, 0)
	is.NoErr(err)

	// Now try to cancel a terminal run
	_, err = fx.Service.CancelRun(ctx, run.ID)
	is.True(errors.Is(err, ci.ErrInvalidTransition))
}

// --- Pickup Timeout Tests (rule PickupTimeout) ---

func TestPickupTimeout_DispatchedRunTimesOut(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareDispatchedRun(t, fx, ctx)

	// Advance time past the pickup timeout
	fx.Clock.Advance(ci.DefaultCIConfig().PickupTimeout + 1*time.Second)

	// Access EnforceTimeouts via concrete type
	pickups, expired, err := fx.Service.EnforceTimeouts(ctx)
	is.NoErr(err)
	is.Equal(pickups, 1)
	is.Equal(expired, 0)

	// Run should now be failed with pickup_timeout
	result, err := fx.Service.GetRun(ctx, run.ID)
	is.NoErr(err)
	is.Equal(result.Status, ci.RunStatusFailed)
	is.True(result.FailureReason != nil)
	is.Equal(*result.FailureReason, ci.FailureReasonPickupTimeout)
	is.True(result.FinishedAt != nil)
}

// --- Run Rotation/Expiry Tests (rule RotateExpiredRuns) ---

func TestRotation_ExpiredTerminalRun_IsDeleted(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	// Create and fully succeed a run
	run := prepareSucceededRun(t, fx, ctx)

	// Advance past run_retention
	fx.Clock.Advance(ci.DefaultCIConfig().RunRetention + 1*time.Second)

	_, expired, err := fx.Service.EnforceTimeouts(ctx)
	is.NoErr(err)
	is.Equal(expired, 1)

	// Run should be gone
	_, err = fx.Service.GetRun(ctx, run.ID)
	is.True(errors.Is(err, ci.ErrRunNotFound))
}

func TestRotation_RunWithinRetention_IsNotDeleted(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareSucceededRun(t, fx, ctx)

	// Advance some time, but not past retention
	fx.Clock.Advance(1 * time.Hour)

	_, expired, err := fx.Service.EnforceTimeouts(ctx)
	is.NoErr(err)
	is.Equal(expired, 0)

	// Run should still exist
	_, err = fx.Service.GetRun(ctx, run.ID)
	is.NoErr(err)
}

func TestRotation_LogEntriesCascadeOnDeletion(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	// Create a run, add log entries, succeed it
	run := prepareRunningRun(t, fx, ctx)
	fx.Service.IngestLogLine(ctx, run.ID, "log 1")
	fx.Service.IngestLogLine(ctx, run.ID, "log 2")
	fx.Service.HandleRunnerCompletion(ctx, run.ID, 0)

	// Advance past retention
	fx.Clock.Advance(ci.DefaultCIConfig().RunRetention + 1*time.Second)

	_, expired, err := fx.Service.EnforceTimeouts(ctx)
	is.NoErr(err)
	is.Equal(expired, 1)

	// Log entries should also be gone
	entries, err := fx.Store.ListLogEntriesByRun(ctx, run.ID)
	is.NoErr(err)
	is.Equal(len(entries), 0)
}

// --- Full Lifecycle Scenario Tests ---

func TestScenario_HappyPath_PendingToSucceeded(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	// 1. Register runner
	_, err := fx.Service.RegisterRunner(ctx, "docker-runner", "https://runner.example.com/dispatch")
	is.NoErr(err)

	// 2. Sync workflows
	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	// 3. Webhook event fires → runs created
	runs, err := fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypePush)
	is.NoErr(err)
	is.Equal(len(runs), 1)

	runID := runs[0].ID
	verifyRunState(t, fx, ctx, runID, ci.RunStatusPending, false, false, false, "")

	// 4. Dispatch → dispatched
	dispatched, err := fx.Service.DispatchRun(ctx, runID)
	is.NoErr(err)
	is.Equal(dispatched.Status, ci.RunStatusDispatched)
	verifyRunState(t, fx, ctx, runID, ci.RunStatusDispatched, false, false, false, "")

	// 5. Runner reports started → running
	running, err := fx.Service.HandleRunnerStarted(ctx, runID)
	is.NoErr(err)
	is.Equal(running.Status, ci.RunStatusRunning)
	verifyRunState(t, fx, ctx, runID, ci.RunStatusRunning, true, false, false, "")

	// 6. Ingest some logs
	fx.Service.IngestLogLine(ctx, runID, "go: downloading...")
	fx.Service.IngestLogLine(ctx, runID, "ok      my/pkg  0.123s")
	entries, _ := fx.Service.ListLogEntries(ctx, runID)
	is.Equal(len(entries), 2)

	// 7. Runner reports completion (exit 0) → succeeded
	succeeded, err := fx.Service.HandleRunnerCompletion(ctx, runID, 0)
	is.NoErr(err)
	is.Equal(succeeded.Status, ci.RunStatusSucceeded)
	verifyRunState(t, fx, ctx, runID, ci.RunStatusSucceeded, true, true, false, "")
}

func TestScenario_ErrorPath_PendingToFailedViaRunnerFailure(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareRunningRun(t, fx, ctx)

	// Runner reports failure
	failed, err := fx.Service.HandleRunnerCompletion(ctx, run.ID, 1)
	is.NoErr(err)
	is.Equal(failed.Status, ci.RunStatusFailed)
	is.Equal(*failed.FailureReason, ci.FailureReasonRunnerReportedFailure)
	verifyRunState(t, fx, ctx, run.ID, ci.RunStatusFailed, true, true, true, ci.FailureReasonRunnerReportedFailure)
}

func TestScenario_ErrorPath_PendingToFailedViaUnknownRunner(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	// Create a run pointing to a non-existent runner
	err := fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "ghost-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	runs, err := fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypePush)
	is.NoErr(err)

	result, err := fx.Service.DispatchRun(ctx, runs[0].ID)
	is.NoErr(err)
	is.Equal(result.Status, ci.RunStatusFailed)
	is.Equal(*result.FailureReason, ci.FailureReasonUnknownRunner)
}

func TestScenario_ErrorPath_PendingToFailedViaDispatchFailure(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	_, err := fx.Service.RegisterRunner(ctx, "docker-runner", "https://runner.example.com/dispatch")
	is.NoErr(err)

	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	runs, err := fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypePush)
	is.NoErr(err)

	fx.Dispatch.DispatchErr = errors.New("timeout")
	result, err := fx.Service.DispatchRun(ctx, runs[0].ID)
	is.NoErr(err)
	is.Equal(result.Status, ci.RunStatusFailed)
	is.Equal(*result.FailureReason, ci.FailureReasonDispatchAckFailed)
	verifyRunState(t, fx, ctx, runs[0].ID, ci.RunStatusFailed, false, true, true, ci.FailureReasonDispatchAckFailed)
}

func TestScenario_CancelPath_PendingToCanceledBeforeDispatch(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := preparePendingRun(t, fx, ctx)

	result, err := fx.Service.CancelRun(ctx, run.ID)
	is.NoErr(err)
	is.Equal(result.Status, ci.RunStatusCanceled)
	verifyRunState(t, fx, ctx, run.ID, ci.RunStatusCanceled, false, true, false, "")
	is.Equal(fx.Dispatch.DispatchCount(), 0) // never dispatched
	is.Equal(fx.Dispatch.CancelCount(), 0)   // no cancel webhook sent
}

func TestScenario_CancelPath_DispatchedToCanceled(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareDispatchedRun(t, fx, ctx)

	result, err := fx.Service.CancelRun(ctx, run.ID)
	is.NoErr(err)
	is.Equal(result.Status, ci.RunStatusCanceled)
	verifyRunState(t, fx, ctx, run.ID, ci.RunStatusCanceled, false, true, false, "")
	is.Equal(fx.Dispatch.CancelCount(), 1)
}

func TestScenario_PickupTimeoutPath(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareDispatchedRun(t, fx, ctx)

	// Time passes, runner never picks up
	fx.Clock.Advance(ci.DefaultCIConfig().PickupTimeout + 1*time.Second)

	pickups, expired, err := fx.Service.EnforceTimeouts(ctx)
	is.NoErr(err)
	is.Equal(pickups, 1)
	is.Equal(expired, 0)

	result, err := fx.Service.GetRun(ctx, run.ID)
	is.NoErr(err)
	is.Equal(result.Status, ci.RunStatusFailed)
	is.Equal(*result.FailureReason, ci.FailureReasonPickupTimeout)
	verifyRunState(t, fx, ctx, run.ID, ci.RunStatusFailed, false, true, true, ci.FailureReasonPickupTimeout)
}

// --- Cross-Rule Interaction Tests ---

func TestCrossRule_DispatchOnlyFromPending(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareDispatchedRun(t, fx, ctx)

	// Try to dispatch an already-dispatched run
	_, err := fx.Service.DispatchRun(ctx, run.ID)
	is.True(err != nil) // should fail
}

func TestCrossRule_StartedOnlyFromDispatched(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareRunningRun(t, fx, ctx)

	// Try to start an already-running run
	_, err := fx.Service.HandleRunnerStarted(ctx, run.ID)
	is.True(errors.Is(err, ci.ErrInvalidTransition))
}

func TestCrossRule_CompletionOnlyFromRunning(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareDispatchedRun(t, fx, ctx)

	// Try to complete a run that hasn't started
	_, err := fx.Service.HandleRunnerCompletion(ctx, run.ID, 0)
	is.True(errors.Is(err, ci.ErrInvalidTransition))
}

func TestCrossRule_LogsOnlyFromRunning(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareDispatchedRun(t, fx, ctx)

	// Try to ingest logs on a dispatched run
	_, err := fx.Service.IngestLogLine(ctx, run.ID, "premature log")
	is.True(errors.Is(err, ci.ErrInvalidTransition))
}

func TestCrossRule_CannotModifyTerminalRun(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareSucceededRun(t, fx, ctx)

	// Try to add logs to a terminal run
	_, err := fx.Service.IngestLogLine(ctx, run.ID, "post-completion log")
	is.True(errors.Is(err, ci.ErrInvalidTransition))

	// Try to report completion again
	_, err = fx.Service.HandleRunnerCompletion(ctx, run.ID, 0)
	is.True(errors.Is(err, ci.ErrInvalidTransition))

	// Try to start
	_, err = fx.Service.HandleRunnerStarted(ctx, run.ID)
	is.True(errors.Is(err, ci.ErrInvalidTransition))
}

// --- Multiple Workflows, Same Event Test ---

func TestMultipleWorkflows_SameEvent_CreatesAllRuns(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	_, err := fx.Service.RegisterRunner(ctx, "docker-runner", "https://runner.example.com/dispatch")
	is.NoErr(err)

	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
		{Name: "lint", Script: "golangci-lint", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
		{Name: "build", Script: "go build", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	runs, err := fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypePush)
	is.NoErr(err)
	is.Equal(len(runs), 3)

	// All runs should be independently manageable
	for _, r := range runs {
		d, err := fx.Service.DispatchRun(ctx, r.ID)
		is.NoErr(err)
		is.Equal(d.Status, ci.RunStatusDispatched)
	}
}

// --- Edge Cases ---

func TestEdgeCase_RunWithNoWorkflows_CreatesNothing(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	// No workflows synced at all
	runs, err := fx.Service.CreateRunsOnEvent(ctx, "empty-repo", ci.EventTypePush)
	is.NoErr(err)
	is.Equal(len(runs), 0)
}

func TestEdgeCase_WorkflowWithMultipleTriggers(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	_, err := fx.Service.RegisterRunner(ctx, "docker-runner", "https://runner.example.com/dispatch")
	is.NoErr(err)

	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "multi-trigger", Script: "echo hi", RunsOn: "docker-runner",
			Triggers: []ci.EventType{ci.EventTypePush, ci.EventTypeBranchTagCreate, ci.EventTypeBranchTagDelete}},
	})
	is.NoErr(err)

	// Fire push → should create run
	runs, _ := fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypePush)
	is.Equal(len(runs), 1)

	// Fire branch_tag_create → should create another run
	runs, _ = fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypeBranchTagCreate)
	is.Equal(len(runs), 1)

	// Fire collaborator → should NOT create
	runs, _ = fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypeCollaborator)
	is.Equal(len(runs), 0)
}

func TestEdgeCase_PickupTimeoutExactBoundary(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareDispatchedRun(t, fx, ctx)

	// Advance exactly to the pickup timeout boundary
	fx.Clock.Advance(ci.DefaultCIConfig().PickupTimeout)

	pickups, _, err := fx.Service.EnforceTimeouts(ctx)
	is.NoErr(err)
	is.Equal(pickups, 1) // exactly at deadline should fire

	result, _ := fx.Service.GetRun(ctx, run.ID)
	is.Equal(result.Status, ci.RunStatusFailed)
}

func TestEdgeCase_PickupTimeoutJustBeforeBoundary(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	run := prepareDispatchedRun(t, fx, ctx)

	// Advance to just before pickup timeout
	fx.Clock.Advance(ci.DefaultCIConfig().PickupTimeout - 1*time.Nanosecond)

	pickups, _, err := fx.Service.EnforceTimeouts(ctx)
	is.NoErr(err)
	is.Equal(pickups, 0) // not yet at deadline

	result, _ := fx.Service.GetRun(ctx, run.ID)
	is.Equal(result.Status, ci.RunStatusDispatched) // still dispatched
}

func TestEdgeCase_NoPickupTimeoutOnNonDispatched(t *testing.T) {
	is := is.New(t)
	fx := newFixture(t)
	ctx := context.Background()

	// Create a pending run (not yet dispatched)
	run := preparePendingRun(t, fx, ctx)

	// Advance past pickup timeout
	fx.Clock.Advance(ci.DefaultCIConfig().PickupTimeout + 1*time.Second)

	pickups, _, err := fx.Service.EnforceTimeouts(ctx)
	is.NoErr(err)
	is.Equal(pickups, 0) // pending runs don't get pickup timeout

	result, _ := fx.Service.GetRun(ctx, run.ID)
	is.Equal(result.Status, ci.RunStatusPending) // still pending
}

// --- Helpers ---

// preparePendingRun creates a runner, workflow, and triggers an event to create a pending run.
func preparePendingRun(t *testing.T, fx *fake.Setup, ctx context.Context) *ci.Run {
	t.Helper()
	is := is.New(t)

	_, err := fx.Service.RegisterRunner(ctx, "docker-runner", "https://runner.example.com/dispatch")
	is.NoErr(err)

	err = fx.Service.SyncWorkflowsOnPush(ctx, "my-repo", []ci.WorkflowDefinition{
		{Name: "test", Script: "go test ./...", RunsOn: "docker-runner", Triggers: []ci.EventType{ci.EventTypePush}},
	})
	is.NoErr(err)

	runs, err := fx.Service.CreateRunsOnEvent(ctx, "my-repo", ci.EventTypePush)
	is.NoErr(err)
	is.Equal(len(runs), 1)

	return &runs[0]
}

// prepareDispatchedRun creates a pending run and dispatches it.
func prepareDispatchedRun(t *testing.T, fx *fake.Setup, ctx context.Context) *ci.Run {
	t.Helper()
	is := is.New(t)

	run := preparePendingRun(t, fx, ctx)
	dispatched, err := fx.Service.DispatchRun(ctx, run.ID)
	is.NoErr(err)
	is.Equal(dispatched.Status, ci.RunStatusDispatched)
	return dispatched
}

// prepareRunningRun creates a dispatched run and has the runner report started.
func prepareRunningRun(t *testing.T, fx *fake.Setup, ctx context.Context) *ci.Run {
	t.Helper()
	is := is.New(t)

	run := prepareDispatchedRun(t, fx, ctx)
	running, err := fx.Service.HandleRunnerStarted(ctx, run.ID)
	is.NoErr(err)
	is.Equal(running.Status, ci.RunStatusRunning)
	return running
}

// prepareSucceededRun creates a running run and has the runner report success.
func prepareSucceededRun(t *testing.T, fx *fake.Setup, ctx context.Context) *ci.Run {
	t.Helper()
	is := is.New(t)

	run := prepareRunningRun(t, fx, ctx)
	succeeded, err := fx.Service.HandleRunnerCompletion(ctx, run.ID, 0)
	is.NoErr(err)
	is.Equal(succeeded.Status, ci.RunStatusSucceeded)
	return succeeded
}

// verifyRunState checks a run's state-dependent fields against expectations.
func verifyRunState(t *testing.T, fx *fake.Setup, ctx context.Context, runID int64,
	expectedStatus ci.RunStatus,
	expectStartedAt, expectFinishedAt, expectFailureReason bool,
	expectedFailureReason ci.FailureReason,
) {
	t.Helper()
	is := is.New(t)

	run, err := fx.Service.GetRun(ctx, runID)
	is.NoErr(err)
	is.Equal(run.Status, expectedStatus)

	if expectStartedAt {
		is.True(run.StartedAt != nil)
	} else {
		is.True(run.StartedAt == nil)
	}

	if expectFinishedAt {
		is.True(run.FinishedAt != nil)
	} else {
		is.True(run.FinishedAt == nil)
	}

	if expectFailureReason {
		is.True(run.FailureReason != nil)
		if expectedFailureReason != "" {
			is.Equal(*run.FailureReason, expectedFailureReason)
		}
	} else {
		is.True(run.FailureReason == nil)
	}
}
