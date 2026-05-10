package ci

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.PickupTimeout != time.Hour {
		t.Fatalf("pickup timeout = %s, want 1h", cfg.PickupTimeout)
	}
	if cfg.RunRetention != 7*24*time.Hour {
		t.Fatalf("run retention = %s, want 7d", cfg.RunRetention)
	}
}

func TestEventType_AllValuesAreValidAndComparable(t *testing.T) {
	seen := make(map[EventType]bool)
	for _, eventType := range []EventType{
		EventTypePush,
		EventTypeBranchTagCreate,
		EventTypeBranchTagDelete,
		EventTypeCollaborator,
		EventTypeRepository,
		EventTypeRepositoryVisibilityChange,
	} {
		seen[eventType] = true
		if !ValidEventTypes[eventType] {
			t.Fatalf("event type %q not marked valid", eventType)
		}
	}

	if ValidEventTypes[EventType("pull_request")] {
		t.Fatal("unknown event type marked valid")
	}
	if len(seen) != len(ValidEventTypes) {
		t.Fatalf("valid event type set has %d values, tests covered %d", len(ValidEventTypes), len(seen))
	}
}

func TestFailureReason_AllValuesAreValidAndComparable(t *testing.T) {
	seen := make(map[FailureReason]bool)
	for _, reason := range []FailureReason{
		FailureReasonDispatchAckFailed,
		FailureReasonPickupTimeout,
		FailureReasonRunnerReportedFailure,
		FailureReasonUnknownRunner,
	} {
		seen[reason] = true
		if !ValidFailureReasons[reason] {
			t.Fatalf("failure reason %q not marked valid", reason)
		}
	}

	if ValidFailureReasons[FailureReason("script_timeout")] {
		t.Fatal("unknown failure reason marked valid")
	}
	if len(seen) != len(ValidFailureReasons) {
		t.Fatalf("valid failure reason set has %d values, tests covered %d", len(ValidFailureReasons), len(seen))
	}
}

func TestRunStatus_AllValuesAreValidAndComparable(t *testing.T) {
	seen := make(map[RunStatus]bool)
	for _, status := range []RunStatus{
		RunPending,
		RunDispatched,
		RunRunning,
		RunSucceeded,
		RunFailed,
		RunCanceled,
	} {
		seen[status] = true
		if !ValidRunStatuses[status] {
			t.Fatalf("run status %q not marked valid", status)
		}
	}

	if ValidRunStatuses[RunStatus("queued")] {
		t.Fatal("unknown run status marked valid")
	}
	if len(seen) != len(ValidRunStatuses) {
		t.Fatalf("valid run status set has %d values, tests covered %d", len(ValidRunStatuses), len(seen))
	}
}

func TestRepoInfo_Fields(t *testing.T) {
	repo := RepoInfo{Name: "example"}

	if repo.Name != "example" {
		t.Fatalf("name = %q", repo.Name)
	}
}

func TestUserInfo_Fields(t *testing.T) {
	user := UserInfo{Role: "admin"}

	if user.Role != "admin" {
		t.Fatalf("role = %q", user.Role)
	}
}

func TestRunnerRegistration_Fields(t *testing.T) {
	registration := RunnerRegistration{
		Name:        "linux-amd64",
		DispatchURL: "https://runner.example.test/dispatch",
		SecretToken: "runner-token",
	}

	if registration.Name != "linux-amd64" {
		t.Fatalf("name = %q", registration.Name)
	}
	if registration.DispatchURL != "https://runner.example.test/dispatch" {
		t.Fatalf("dispatch URL = %q", registration.DispatchURL)
	}
	if registration.SecretToken != "runner-token" {
		t.Fatalf("secret token = %q", registration.SecretToken)
	}
}

func TestWorkflow_FieldsAndOptionalContainer(t *testing.T) {
	container := "ghcr.io/charmbracelet/soft-serve-ci:latest"
	workflow := Workflow{
		RepoName:  "example",
		Name:      "test",
		Script:    "go test ./...",
		RunsOn:    "linux-amd64",
		Container: &container,
		Triggers: map[EventType]bool{
			EventTypePush: true,
		},
	}

	if workflow.RepoName != "example" {
		t.Fatalf("repo name = %q", workflow.RepoName)
	}
	if workflow.Name != "test" {
		t.Fatalf("name = %q", workflow.Name)
	}
	if workflow.Script != "go test ./..." {
		t.Fatalf("script = %q", workflow.Script)
	}
	if workflow.RunsOn != "linux-amd64" {
		t.Fatalf("runs_on = %q", workflow.RunsOn)
	}
	if workflow.Container == nil || *workflow.Container != container {
		t.Fatalf("container = %v", workflow.Container)
	}
	if !workflow.Triggers[EventTypePush] {
		t.Fatal("push trigger not present")
	}

	workflow.Container = nil
	if workflow.Container != nil {
		t.Fatal("optional container should accept nil")
	}
}

func TestRun_FieldsAndOptionalValues(t *testing.T) {
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	startedAt := now.Add(2 * time.Minute)
	finishedAt := now.Add(5 * time.Minute)
	reason := FailureReasonRunnerReportedFailure
	container := "ubuntu:24.04"
	run := Run{
		ID:               12,
		RepoName:         "example",
		WorkflowName:     "test",
		Script:           "go test ./...",
		RunsOn:           "linux-amd64",
		Container:        &container,
		TriggeredByEvent: EventTypePush,
		Status:           RunFailed,
		CreatedAt:        now,
		StartedAt:        &startedAt,
		FinishedAt:       &finishedAt,
		FailureReason:    &reason,
	}

	if run.RepoName != "example" || run.WorkflowName != "test" {
		t.Fatalf("repo/workflow = %q/%q", run.RepoName, run.WorkflowName)
	}
	if run.Script != "go test ./..." || run.RunsOn != "linux-amd64" {
		t.Fatalf("script/runs_on = %q/%q", run.Script, run.RunsOn)
	}
	if run.Container == nil || *run.Container != container {
		t.Fatalf("container = %v", run.Container)
	}
	if run.TriggeredByEvent != EventTypePush {
		t.Fatalf("triggered_by_event = %q", run.TriggeredByEvent)
	}
	if run.Status != RunFailed {
		t.Fatalf("status = %q", run.Status)
	}
	if !run.CreatedAt.Equal(now) {
		t.Fatalf("created_at = %s", run.CreatedAt)
	}
	if run.StartedAt == nil || !run.StartedAt.Equal(startedAt) {
		t.Fatalf("started_at = %v", run.StartedAt)
	}
	if run.FinishedAt == nil || !run.FinishedAt.Equal(finishedAt) {
		t.Fatalf("finished_at = %v", run.FinishedAt)
	}
	if run.FailureReason == nil || *run.FailureReason != FailureReasonRunnerReportedFailure {
		t.Fatalf("failure_reason = %v", run.FailureReason)
	}

	run.Container = nil
	run.StartedAt = nil
	run.FinishedAt = nil
	run.FailureReason = nil
	if run.Container != nil || run.StartedAt != nil || run.FinishedAt != nil || run.FailureReason != nil {
		t.Fatal("optional run fields should accept nil")
	}
}

func TestLogEntry_Fields(t *testing.T) {
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	entry := LogEntry{
		ID:         33,
		RunID:      12,
		Line:       "ok github.com/charmbracelet/soft-serve/pkg/ci",
		ReceivedAt: now,
	}

	if entry.RunID != 12 {
		t.Fatalf("run id = %d", entry.RunID)
	}
	if entry.Line != "ok github.com/charmbracelet/soft-serve/pkg/ci" {
		t.Fatalf("line = %q", entry.Line)
	}
	if !entry.ReceivedAt.Equal(now) {
		t.Fatalf("received_at = %s", entry.ReceivedAt)
	}
}

func TestRunTransitionGraph_AllowsDeclaredEdges(t *testing.T) {
	tests := []struct {
		name string
		from RunStatus
		to   RunStatus
	}{
		{"pending to dispatched", RunPending, RunDispatched},
		{"pending to failed", RunPending, RunFailed},
		{"pending to canceled", RunPending, RunCanceled},
		{"dispatched to failed", RunDispatched, RunFailed},
		{"dispatched to running", RunDispatched, RunRunning},
		{"dispatched to canceled", RunDispatched, RunCanceled},
		{"running to succeeded", RunRunning, RunSucceeded},
		{"running to failed", RunRunning, RunFailed},
		{"running to canceled", RunRunning, RunCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := &Run{Status: tt.from}
			if !run.CanTransition(tt.to) {
				t.Fatalf("%s -> %s should be allowed", tt.from, tt.to)
			}
		})
	}
}

func TestRunTransitionGraph_RejectsUndeclaredEdges(t *testing.T) {
	tests := []struct {
		name string
		from RunStatus
		to   RunStatus
	}{
		{"pending to running", RunPending, RunRunning},
		{"pending to succeeded", RunPending, RunSucceeded},
		{"dispatched to succeeded", RunDispatched, RunSucceeded},
		{"running to dispatched", RunRunning, RunDispatched},
		{"running to pending", RunRunning, RunPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := &Run{Status: tt.from}
			if run.CanTransition(tt.to) {
				t.Fatalf("%s -> %s should be rejected", tt.from, tt.to)
			}
		})
	}
}

func TestRunTransitionGraph_TerminalStatesHaveNoOutboundEdges(t *testing.T) {
	for _, terminal := range []RunStatus{RunSucceeded, RunFailed, RunCanceled} {
		t.Run(string(terminal), func(t *testing.T) {
			if !terminal.IsTerminal() {
				t.Fatalf("%s should be terminal", terminal)
			}
			for _, next := range []RunStatus{RunPending, RunDispatched, RunRunning, RunSucceeded, RunFailed, RunCanceled} {
				run := &Run{Status: terminal}
				if run.CanTransition(next) {
					t.Fatalf("%s -> %s should be rejected because %s is terminal", terminal, next, terminal)
				}
			}
		})
	}
}

func TestService_RegisterRunner_RequiresAdminAndStoresGeneratedToken(t *testing.T) {
	ctx := context.Background()
	service, store, _, _, tokens, _ := newTestService(t)
	tokens.next = "generated-runner-token"

	if _, err := service.RegisterRunner(ctx, UserInfo{Role: "user"}, "linux-amd64", "https://runner.example.test/dispatch"); !errors.Is(err, ErrNotAdmin) {
		t.Fatalf("non-admin register error = %v, want %v", err, ErrNotAdmin)
	}

	registration, err := service.RegisterRunner(ctx, UserInfo{Role: "admin"}, "linux-amd64", "https://runner.example.test/dispatch")
	if err != nil {
		t.Fatalf("register runner: %v", err)
	}
	if registration.SecretToken != "generated-runner-token" {
		t.Fatalf("secret token = %q", registration.SecretToken)
	}

	stored, err := store.GetRunnerRegistration(ctx, "linux-amd64")
	if err != nil {
		t.Fatalf("get runner registration: %v", err)
	}
	if stored.DispatchURL != "https://runner.example.test/dispatch" || stored.SecretToken != "generated-runner-token" {
		t.Fatalf("stored registration = %#v", stored)
	}
}

func TestService_SyncWorkflowsOnPush_ReconcilesMagicFolder(t *testing.T) {
	ctx := context.Background()
	service, store, workflowSource, _, _, _ := newTestService(t)
	store.putWorkflow(t, Workflow{RepoName: "repo1", Name: "stale", Script: "old", RunsOn: "linux-amd64"})
	container := "golang:1.25"
	workflowSource.definitions = []WorkflowDefinition{
		{
			Name:      "unit",
			Script:    "go test ./...",
			RunsOn:    "linux-amd64",
			Container: &container,
			Triggers:  map[EventType]bool{EventTypePush: true},
		},
		{
			Name:     "release",
			Script:   "go test ./pkg/webhook",
			RunsOn:   "linux-arm64",
			Triggers: map[EventType]bool{EventTypeRepository: true},
		},
	}

	if err := service.SyncWorkflowsOnPush(ctx, "repo1"); err != nil {
		t.Fatalf("sync workflows: %v", err)
	}

	workflows, err := store.ListWorkflowsByRepo(ctx, "repo1")
	if err != nil {
		t.Fatalf("list workflows: %v", err)
	}
	if len(workflows) != 2 {
		t.Fatalf("workflow count = %d, want 2: %#v", len(workflows), workflows)
	}
	if got := findWorkflow(t, workflows, "unit"); got.Script != "go test ./..." || got.RunsOn != "linux-amd64" {
		t.Fatalf("unit workflow = %#v", got)
	}
	if got := findWorkflow(t, workflows, "unit"); got.Container == nil || *got.Container != container {
		t.Fatalf("unit container = %v", got.Container)
	}
	if got := findWorkflow(t, workflows, "release"); got.Container != nil {
		t.Fatalf("release container = %v, want nil", got.Container)
	}
	if findWorkflowMaybe(workflows, "stale") != nil {
		t.Fatal("stale workflow was not removed")
	}
}

func TestService_SyncWorkflowsOnPush_ParseErrorRejectsPushAndDoesNotChangeStoredWorkflows(t *testing.T) {
	ctx := context.Background()
	service, store, workflowSource, _, _, _ := newTestService(t)
	existing := Workflow{RepoName: "repo1", Name: "unit", Script: "go test ./...", RunsOn: "linux-amd64"}
	store.putWorkflow(t, existing)
	workflowSource.err = ErrWorkflowParse

	if err := service.SyncWorkflowsOnPush(ctx, "repo1"); !errors.Is(err, ErrWorkflowParse) {
		t.Fatalf("sync workflows error = %v, want %v", err, ErrWorkflowParse)
	}

	workflows, err := store.ListWorkflowsByRepo(ctx, "repo1")
	if err != nil {
		t.Fatalf("list workflows: %v", err)
	}
	if len(workflows) != 1 || workflows[0].Name != existing.Name || workflows[0].Script != existing.Script {
		t.Fatalf("stored workflows changed after parse rejection: %#v", workflows)
	}
}

func TestService_ValidateWorkflowsAtCommit_ReturnsParseErrorAndDoesNotTouchStore(t *testing.T) {
	ctx := context.Background()
	service, store, workflowSource, _, _, _ := newTestService(t)
	existing := Workflow{RepoName: "repo1", Name: "unit", Script: "go test ./...", RunsOn: "linux-amd64"}
	store.putWorkflow(t, existing)
	workflowSource.commitParseErr = ErrWorkflowParse

	err := service.ValidateWorkflowsAtCommit(ctx, "repo1", "deadbeef")
	if !errors.Is(err, ErrWorkflowParse) {
		t.Fatalf("validate at commit err = %v, want %v", err, ErrWorkflowParse)
	}
	if len(workflowSource.commitParseCalls) != 1 || workflowSource.commitParseCalls[0].CommitSHA != "deadbeef" {
		t.Fatalf("expected one parse call against deadbeef, got %#v", workflowSource.commitParseCalls)
	}

	workflows, err := store.ListWorkflowsByRepo(ctx, "repo1")
	if err != nil {
		t.Fatalf("list workflows: %v", err)
	}
	if len(workflows) != 1 || workflows[0].Name != existing.Name {
		t.Fatalf("validation must not touch stored workflows: %#v", workflows)
	}
}

func TestService_ValidateWorkflowsAtCommit_OkWhenParseSucceeds(t *testing.T) {
	ctx := context.Background()
	service, _, workflowSource, _, _, _ := newTestService(t)
	workflowSource.definitions = []WorkflowDefinition{
		{Name: "unit", Script: "go test ./...", RunsOn: "linux-amd64", Triggers: map[EventType]bool{EventTypePush: true}},
	}

	if err := service.ValidateWorkflowsAtCommit(ctx, "repo1", "cafef00d"); err != nil {
		t.Fatalf("validate at commit: %v", err)
	}
}

func TestService_HandleWebhookEvent_CreatesPendingRunsForMatchingWorkflowTriggers(t *testing.T) {
	ctx := context.Background()
	service, store, _, _, _, clock := newTestService(t)
	container := "golang:1.25"
	store.putWorkflow(t, Workflow{
		RepoName:  "repo1",
		Name:      "unit",
		Script:    "go test ./...",
		RunsOn:    "linux-amd64",
		Container: &container,
		Triggers:  map[EventType]bool{EventTypePush: true},
	})
	store.putWorkflow(t, Workflow{
		RepoName: "repo1",
		Name:     "release",
		Script:   "go test ./pkg/webhook",
		RunsOn:   "linux-arm64",
		Triggers: map[EventType]bool{EventTypeRepository: true},
	})

	if err := service.HandleWebhookEvent(ctx, "repo1", EventTypePush); err != nil {
		t.Fatalf("handle webhook event: %v", err)
	}

	runs := store.runsForRepo("repo1")
	if len(runs) != 1 {
		t.Fatalf("run count = %d, want 1: %#v", len(runs), runs)
	}
	run := runs[0]
	if run.WorkflowName != "unit" || run.Script != "go test ./..." || run.RunsOn != "linux-amd64" {
		t.Fatalf("run snapshot = %#v", run)
	}
	if run.Container == nil || *run.Container != container {
		t.Fatalf("container = %v", run.Container)
	}
	if run.TriggeredByEvent != EventTypePush {
		t.Fatalf("triggered event = %q", run.TriggeredByEvent)
	}
	if run.Status != RunPending {
		t.Fatalf("status = %q, want pending", run.Status)
	}
	if !run.CreatedAt.Equal(clock.Now()) {
		t.Fatalf("created_at = %s, want %s", run.CreatedAt, clock.Now())
	}
}

func TestService_HandleWebhookEvent_SnapshotsWorkflowDefinitionAtTriggerTime(t *testing.T) {
	ctx := context.Background()
	service, store, _, _, _, _ := newTestService(t)
	store.putWorkflow(t, Workflow{
		RepoName: "repo1",
		Name:     "unit",
		Script:   "go test ./...",
		RunsOn:   "linux-amd64",
		Triggers: map[EventType]bool{EventTypePush: true},
	})

	if err := service.HandleWebhookEvent(ctx, "repo1", EventTypePush); err != nil {
		t.Fatalf("handle webhook event: %v", err)
	}
	store.putWorkflow(t, Workflow{
		RepoName: "repo1",
		Name:     "unit",
		Script:   "exit 1",
		RunsOn:   "linux-arm64",
		Triggers: map[EventType]bool{EventTypePush: true},
	})

	runs := store.runsForRepo("repo1")
	if len(runs) != 1 {
		t.Fatalf("run count = %d, want 1", len(runs))
	}
	if runs[0].Script != "go test ./..." || runs[0].RunsOn != "linux-amd64" {
		t.Fatalf("run did not preserve trigger-time workflow snapshot: %#v", runs[0])
	}
}

func TestService_DispatchPendingRun_DispatchedOnRunnerAck(t *testing.T) {
	ctx := context.Background()
	service, store, _, dispatcher, _, _ := newTestService(t)
	store.putRunnerRegistration(t, RunnerRegistration{
		Name:        "linux-amd64",
		DispatchURL: "https://runner.example.test/dispatch",
		SecretToken: "runner-token",
	})
	run := store.insertRun(t, Run{
		RepoName:         "repo1",
		WorkflowName:     "unit",
		Script:           "go test ./...",
		RunsOn:           "linux-amd64",
		TriggeredByEvent: EventTypePush,
		Status:           RunPending,
		CreatedAt:        testNow(),
	})

	if err := service.DispatchPendingRun(ctx, run.ID); err != nil {
		t.Fatalf("dispatch pending run: %v", err)
	}

	got := store.getRun(t, run.ID)
	if got.Status != RunDispatched {
		t.Fatalf("status = %q, want dispatched", got.Status)
	}
	if len(dispatcher.dispatches) != 1 {
		t.Fatalf("dispatch count = %d, want 1", len(dispatcher.dispatches))
	}
	if dispatcher.dispatches[0].Registration.Name != "linux-amd64" || dispatcher.dispatches[0].Run.ID != run.ID {
		t.Fatalf("dispatch = %#v", dispatcher.dispatches[0])
	}
}

func TestService_DispatchPendingRun_UnknownRunnerFailsRun(t *testing.T) {
	ctx := context.Background()
	service, store, _, dispatcher, _, clock := newTestService(t)
	run := store.insertRun(t, Run{
		RepoName:         "repo1",
		WorkflowName:     "unit",
		Script:           "go test ./...",
		RunsOn:           "missing-runner",
		TriggeredByEvent: EventTypePush,
		Status:           RunPending,
		CreatedAt:        clock.Now(),
	})

	if err := service.DispatchPendingRun(ctx, run.ID); err != nil {
		t.Fatalf("dispatch pending run: %v", err)
	}

	got := store.getRun(t, run.ID)
	assertRunFailed(t, got, FailureReasonUnknownRunner, clock.Now())
	if len(dispatcher.dispatches) != 0 {
		t.Fatalf("dispatch count = %d, want 0", len(dispatcher.dispatches))
	}
}

func TestService_DispatchPendingRun_AckFailureFailsRun(t *testing.T) {
	ctx := context.Background()
	service, store, _, dispatcher, _, clock := newTestService(t)
	dispatcher.dispatchErr = errors.New("runner returned 500")
	store.putRunnerRegistration(t, RunnerRegistration{Name: "linux-amd64", DispatchURL: "https://runner.example.test/dispatch", SecretToken: "runner-token"})
	run := store.insertRun(t, Run{
		RepoName:         "repo1",
		WorkflowName:     "unit",
		Script:           "go test ./...",
		RunsOn:           "linux-amd64",
		TriggeredByEvent: EventTypePush,
		Status:           RunPending,
		CreatedAt:        clock.Now(),
	})

	if err := service.DispatchPendingRun(ctx, run.ID); err != nil {
		t.Fatalf("dispatch pending run: %v", err)
	}

	got := store.getRun(t, run.ID)
	assertRunFailed(t, got, FailureReasonDispatchAckFailed, clock.Now())
}

func TestService_ReportStarted_RequiresAssignedRunnerToken(t *testing.T) {
	ctx := context.Background()
	service, store, _, _, _, _ := newTestService(t)
	store.putRunnerRegistration(t, RunnerRegistration{Name: "linux-amd64", SecretToken: "runner-token"})
	run := store.insertRun(t, Run{RepoName: "repo1", RunsOn: "linux-amd64", Status: RunDispatched, CreatedAt: testNow()})

	if err := service.ReportStarted(ctx, "wrong-token", run.ID); !errors.Is(err, ErrUnauthorizedRunner) {
		t.Fatalf("report started error = %v, want %v", err, ErrUnauthorizedRunner)
	}

	got := store.getRun(t, run.ID)
	if got.Status != RunDispatched {
		t.Fatalf("status = %q, want dispatched", got.Status)
	}
	if got.StartedAt != nil {
		t.Fatalf("started_at = %v, want nil", got.StartedAt)
	}
}

func TestService_ReportStarted_MovesDispatchedRunToRunningAtClockTime(t *testing.T) {
	ctx := context.Background()
	service, store, _, _, _, clock := newTestService(t)
	store.putRunnerRegistration(t, RunnerRegistration{Name: "linux-amd64", SecretToken: "runner-token"})
	run := store.insertRun(t, Run{RepoName: "repo1", RunsOn: "linux-amd64", Status: RunDispatched, CreatedAt: clock.Now()})
	clock.Advance(3 * time.Minute)

	if err := service.ReportStarted(ctx, "runner-token", run.ID); err != nil {
		t.Fatalf("report started: %v", err)
	}

	got := store.getRun(t, run.ID)
	if got.Status != RunRunning {
		t.Fatalf("status = %q, want running", got.Status)
	}
	if got.StartedAt == nil || !got.StartedAt.Equal(clock.Now()) {
		t.Fatalf("started_at = %v, want %s", got.StartedAt, clock.Now())
	}
}

func TestService_ReportCompletion_RecordsSuccessOrFailure(t *testing.T) {
	tests := []struct {
		name       string
		exitCode   int
		wantStatus RunStatus
		wantReason *FailureReason
	}{
		{name: "success", exitCode: 0, wantStatus: RunSucceeded},
		{name: "failure", exitCode: 2, wantStatus: RunFailed, wantReason: ptr(FailureReasonRunnerReportedFailure)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			service, store, _, _, _, clock := newTestService(t)
			store.putRunnerRegistration(t, RunnerRegistration{Name: "linux-amd64", SecretToken: "runner-token"})
			run := store.insertRun(t, Run{RepoName: "repo1", RunsOn: "linux-amd64", Status: RunRunning, CreatedAt: clock.Now(), StartedAt: ptr(clock.Now())})
			clock.Advance(5 * time.Minute)

			if err := service.ReportCompletion(ctx, "runner-token", run.ID, tt.exitCode); err != nil {
				t.Fatalf("report completion: %v", err)
			}

			got := store.getRun(t, run.ID)
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.FinishedAt == nil || !got.FinishedAt.Equal(clock.Now()) {
				t.Fatalf("finished_at = %v, want %s", got.FinishedAt, clock.Now())
			}
			if tt.wantReason == nil {
				if got.FailureReason != nil {
					t.Fatalf("failure_reason = %v, want nil", got.FailureReason)
				}
				return
			}
			if got.FailureReason == nil || *got.FailureReason != *tt.wantReason {
				t.Fatalf("failure_reason = %v, want %v", got.FailureReason, tt.wantReason)
			}
		})
	}
}

func TestService_IngestLogLine_OnlyRunningRunsAcceptLogs(t *testing.T) {
	ctx := context.Background()
	service, store, _, _, _, clock := newTestService(t)
	store.putRunnerRegistration(t, RunnerRegistration{Name: "linux-amd64", SecretToken: "runner-token"})
	run := store.insertRun(t, Run{RepoName: "repo1", RunsOn: "linux-amd64", Status: RunDispatched, CreatedAt: clock.Now()})

	if err := service.IngestLogLine(ctx, "runner-token", run.ID, "not yet"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("ingest before running error = %v, want %v", err, ErrInvalidTransition)
	}
	if len(store.logsForRun(run.ID)) != 0 {
		t.Fatal("log entry created for non-running run")
	}

	store.updateRun(t, Run{ID: run.ID, RepoName: "repo1", RunsOn: "linux-amd64", Status: RunRunning, CreatedAt: clock.Now(), StartedAt: ptr(clock.Now())})
	clock.Advance(2 * time.Second)
	if err := service.IngestLogLine(ctx, "runner-token", run.ID, "go test ./..."); err != nil {
		t.Fatalf("ingest log line: %v", err)
	}

	logs := store.logsForRun(run.ID)
	if len(logs) != 1 {
		t.Fatalf("log count = %d, want 1", len(logs))
	}
	if logs[0].Line != "go test ./..." || !logs[0].ReceivedAt.Equal(clock.Now()) {
		t.Fatalf("log entry = %#v", logs[0])
	}
}

func TestService_CancelPendingRun_CancelsWithoutCallingRunner(t *testing.T) {
	ctx := context.Background()
	service, store, _, dispatcher, _, clock := newTestService(t)
	run := store.insertRun(t, Run{RepoName: "repo1", RunsOn: "linux-amd64", Status: RunPending, CreatedAt: clock.Now()})

	if err := service.CancelRun(ctx, UserInfo{Role: "writer", Username: "writer"}, run.ID); err != nil {
		t.Fatalf("cancel run: %v", err)
	}

	got := store.getRun(t, run.ID)
	if got.Status != RunCanceled {
		t.Fatalf("status = %q, want canceled", got.Status)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(clock.Now()) {
		t.Fatalf("finished_at = %v, want %s", got.FinishedAt, clock.Now())
	}
	if len(dispatcher.cancellations) != 0 {
		t.Fatalf("cancel dispatch count = %d, want 0", len(dispatcher.cancellations))
	}
}

func TestService_CancelDispatchedRun_OnlyCancelsAfterRunnerAck(t *testing.T) {
	ctx := context.Background()
	service, store, _, dispatcher, _, clock := newTestService(t)
	store.putRunnerRegistration(t, RunnerRegistration{Name: "linux-amd64", SecretToken: "runner-token"})
	run := store.insertRun(t, Run{RepoName: "repo1", RunsOn: "linux-amd64", Status: RunDispatched, CreatedAt: clock.Now()})

	dispatcher.cancelErr = errors.New("runner cancel failed")
	if err := service.CancelRun(ctx, UserInfo{Role: "writer", Username: "writer"}, run.ID); !errors.Is(err, dispatcher.cancelErr) {
		t.Fatalf("cancel run error = %v, want %v", err, dispatcher.cancelErr)
	}
	if got := store.getRun(t, run.ID); got.Status != RunDispatched {
		t.Fatalf("status after failed cancel = %q, want dispatched", got.Status)
	}

	dispatcher.cancelErr = nil
	clock.Advance(time.Minute)
	if err := service.CancelRun(ctx, UserInfo{Role: "writer", Username: "writer"}, run.ID); err != nil {
		t.Fatalf("cancel run after ack: %v", err)
	}
	got := store.getRun(t, run.ID)
	if got.Status != RunCanceled {
		t.Fatalf("status = %q, want canceled", got.Status)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(clock.Now()) {
		t.Fatalf("finished_at = %v, want %s", got.FinishedAt, clock.Now())
	}
}

func TestService_CancelRun_RejectsUserWithoutWriteAccess(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	ws := &fakeWorkflowSource{}
	disp := &fakeRunnerDispatcher{}
	tokens := &fakeTokenGenerator{}
	access := &fakeRepoAccessChecker{}
	clock := &fakeClock{now: testNow()}

	service := NewService(DefaultConfig(), store, ws, disp, tokens, access, clock, nil)

	run := store.insertRun(t, Run{RepoName: "myrepo", RunsOn: "linux-amd64", Status: RunPending, CreatedAt: clock.Now()})

	// User without write access should be rejected.
	err := service.CancelRun(ctx, UserInfo{Role: "user", Username: "bob"}, run.ID)
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("cancel run error = %v, want %v", err, ErrNotAuthorized)
	}

	// Verify the run was NOT canceled.
	got := store.getRun(t, run.ID)
	if got.Status != RunPending {
		t.Fatalf("status changed to %q, want pending", got.Status)
	}

	// User with writer username should succeed.
	if err := service.CancelRun(ctx, UserInfo{Role: "writer", Username: "writer"}, run.ID); err != nil {
		t.Fatalf("cancel run for writer: %v", err)
	}

	got = store.getRun(t, run.ID)
	if got.Status != RunCanceled {
		t.Fatalf("status = %q, want canceled", got.Status)
	}
}

func TestService_EnforceTimeouts_PickupTimeoutFiresAtDeadline(t *testing.T) {
	ctx := context.Background()
	service, store, _, _, _, clock := newTestService(t)
	cfg := DefaultConfig()
	run := store.insertRun(t, Run{RepoName: "repo1", RunsOn: "linux-amd64", Status: RunDispatched, CreatedAt: clock.Now()})

	clock.Advance(cfg.PickupTimeout - time.Nanosecond)
	if err := service.EnforceTimeouts(ctx); err != nil {
		t.Fatalf("enforce timeouts before deadline: %v", err)
	}
	if got := store.getRun(t, run.ID); got.Status != RunDispatched {
		t.Fatalf("status before deadline = %q, want dispatched", got.Status)
	}

	clock.Advance(time.Nanosecond)
	if err := service.EnforceTimeouts(ctx); err != nil {
		t.Fatalf("enforce timeouts at deadline: %v", err)
	}
	assertRunFailed(t, store.getRun(t, run.ID), FailureReasonPickupTimeout, clock.Now())
}

func TestService_EnforceTimeouts_PickupTimeoutDoesNotRefireForTerminalRun(t *testing.T) {
	ctx := context.Background()
	service, store, _, _, _, clock := newTestService(t)
	cfg := DefaultConfig()
	run := store.insertRun(t, Run{RepoName: "repo1", RunsOn: "linux-amd64", Status: RunDispatched, CreatedAt: clock.Now()})
	clock.Advance(cfg.PickupTimeout)

	if err := service.EnforceTimeouts(ctx); err != nil {
		t.Fatalf("first enforce timeouts: %v", err)
	}
	finishedAt := *store.getRun(t, run.ID).FinishedAt
	clock.Advance(24 * time.Hour)
	if err := service.EnforceTimeouts(ctx); err != nil {
		t.Fatalf("second enforce timeouts: %v", err)
	}

	got := store.getRun(t, run.ID)
	if got.FinishedAt == nil || !got.FinishedAt.Equal(finishedAt) {
		t.Fatalf("finished_at changed on refire: got %v want %s", got.FinishedAt, finishedAt)
	}
}

func TestService_EnforceTimeouts_StartedRunWinsPickupTimeoutRace(t *testing.T) {
	ctx := context.Background()
	service, store, _, _, _, clock := newTestService(t)
	cfg := DefaultConfig()
	store.putRunnerRegistration(t, RunnerRegistration{Name: "linux-amd64", SecretToken: "runner-token"})
	run := store.insertRun(t, Run{RepoName: "repo1", RunsOn: "linux-amd64", Status: RunDispatched, CreatedAt: clock.Now()})
	clock.Advance(cfg.PickupTimeout)

	if err := service.ReportStarted(ctx, "runner-token", run.ID); err != nil {
		t.Fatalf("report started at timeout boundary: %v", err)
	}
	if err := service.EnforceTimeouts(ctx); err != nil {
		t.Fatalf("enforce timeouts after start: %v", err)
	}

	got := store.getRun(t, run.ID)
	if got.Status != RunRunning {
		t.Fatalf("status = %q, want running", got.Status)
	}
	if got.FailureReason != nil || got.FinishedAt != nil {
		t.Fatalf("run should not be failed by pickup timeout after start: %#v", got)
	}
}

func TestService_EnforceTimeouts_CancelAckWinsPickupTimeoutRace(t *testing.T) {
	ctx := context.Background()
	service, store, _, _, _, clock := newTestService(t)
	cfg := DefaultConfig()
	store.putRunnerRegistration(t, RunnerRegistration{Name: "linux-amd64", SecretToken: "runner-token"})
	run := store.insertRun(t, Run{RepoName: "repo1", RunsOn: "linux-amd64", Status: RunDispatched, CreatedAt: clock.Now()})
	clock.Advance(cfg.PickupTimeout)

	if err := service.CancelRun(ctx, UserInfo{Role: "writer", Username: "writer"}, run.ID); err != nil {
		t.Fatalf("cancel run at timeout boundary: %v", err)
	}
	if err := service.EnforceTimeouts(ctx); err != nil {
		t.Fatalf("enforce timeouts after cancel: %v", err)
	}

	got := store.getRun(t, run.ID)
	if got.Status != RunCanceled {
		t.Fatalf("status = %q, want canceled", got.Status)
	}
	if got.FailureReason != nil {
		t.Fatalf("failure_reason = %v, want nil", got.FailureReason)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(clock.Now()) {
		t.Fatalf("finished_at = %v, want %s", got.FinishedAt, clock.Now())
	}
}

func TestService_RotateExpiredRuns_RemovesTerminalRunsAndLogEntriesAtRetentionDeadline(t *testing.T) {
	ctx := context.Background()
	service, store, _, _, _, clock := newTestService(t)
	cfg := DefaultConfig()
	finishedAt := clock.Now()
	run := store.insertRun(t, Run{RepoName: "repo1", RunsOn: "linux-amd64", Status: RunSucceeded, CreatedAt: finishedAt.Add(-time.Hour), FinishedAt: &finishedAt})
	store.putLogEntry(t, LogEntry{RunID: run.ID, Line: "done", ReceivedAt: finishedAt})

	clock.Advance(cfg.RunRetention - time.Nanosecond)
	if err := service.RotateExpiredRuns(ctx); err != nil {
		t.Fatalf("rotate before deadline: %v", err)
	}
	if store.findRun(run.ID) == nil {
		t.Fatal("run removed before retention deadline")
	}
	if len(store.logsForRun(run.ID)) != 1 {
		t.Fatal("logs removed before retention deadline")
	}

	clock.Advance(time.Nanosecond)
	if err := service.RotateExpiredRuns(ctx); err != nil {
		t.Fatalf("rotate at deadline: %v", err)
	}
	if store.findRun(run.ID) != nil {
		t.Fatal("run still present at retention deadline")
	}
	if len(store.logsForRun(run.ID)) != 0 {
		t.Fatal("logs still present after run rotation")
	}
}

func TestService_RotateExpiredRuns_IgnoresNonTerminalRuns(t *testing.T) {
	ctx := context.Background()
	service, store, _, _, _, clock := newTestService(t)
	cfg := DefaultConfig()
	run := store.insertRun(t, Run{RepoName: "repo1", RunsOn: "linux-amd64", Status: RunRunning, CreatedAt: clock.Now().Add(-2 * cfg.RunRetention)})

	clock.Advance(3 * cfg.RunRetention)
	if err := service.RotateExpiredRuns(ctx); err != nil {
		t.Fatalf("rotate expired runs: %v", err)
	}
	if store.findRun(run.ID) == nil {
		t.Fatal("non-terminal run was rotated")
	}
}

func newTestService(t *testing.T) (*Service, *fakeStore, *fakeWorkflowSource, *fakeRunnerDispatcher, *fakeTokenGenerator, *fakeClock) {
	t.Helper()

	store := newFakeStore()
	workflowSource := &fakeWorkflowSource{}
	dispatcher := &fakeRunnerDispatcher{}
	tokens := &fakeTokenGenerator{next: "test-runner-token"}
	access := &fakeRepoAccessChecker{allowAll: true}
	clock := &fakeClock{now: testNow()}
	service := NewService(DefaultConfig(), store, workflowSource, dispatcher, tokens, access, clock, nil)

	return service, store, workflowSource, dispatcher, tokens, clock
}

func testNow() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}

type fakeTokenGenerator struct {
	next string
	err  error
}

func (g *fakeTokenGenerator) NewToken() (string, error) {
	if g.err != nil {
		return "", g.err
	}
	return g.next, nil
}

type fakeWorkflowSource struct {
	definitions       []WorkflowDefinition
	err               error
	parseCalls        []string
	commitParseErr    error
	commitParseCalls  []commitParseCall
}

type commitParseCall struct {
	RepoName  string
	CommitSHA string
}

func (s *fakeWorkflowSource) ParseMagicFolder(_ context.Context, repoName string) ([]WorkflowDefinition, error) {
	s.parseCalls = append(s.parseCalls, repoName)
	if s.err != nil {
		return nil, s.err
	}
	out := make([]WorkflowDefinition, len(s.definitions))
	copy(out, s.definitions)
	return out, nil
}

func (s *fakeWorkflowSource) ParseMagicFolderAtCommit(_ context.Context, repoName, commitSHA string) ([]WorkflowDefinition, error) {
	s.commitParseCalls = append(s.commitParseCalls, commitParseCall{RepoName: repoName, CommitSHA: commitSHA})
	if s.commitParseErr != nil {
		return nil, s.commitParseErr
	}
	out := make([]WorkflowDefinition, len(s.definitions))
	copy(out, s.definitions)
	return out, nil
}

type fakeRunnerDispatcher struct {
	dispatchErr   error
	cancelErr     error
	dispatches    []dispatchCall
	cancellations []dispatchCall
}

type dispatchCall struct {
	Registration RunnerRegistration
	Run          Run
}

func (d *fakeRunnerDispatcher) DispatchRun(_ context.Context, registration RunnerRegistration, run Run) error {
	if d.dispatchErr != nil {
		return d.dispatchErr
	}
	d.dispatches = append(d.dispatches, dispatchCall{Registration: registration, Run: cloneRun(run)})
	return nil
}

func (d *fakeRunnerDispatcher) CancelRun(_ context.Context, registration RunnerRegistration, run Run) error {
	if d.cancelErr != nil {
		return d.cancelErr
	}
	d.cancellations = append(d.cancellations, dispatchCall{Registration: registration, Run: cloneRun(run)})
	return nil
}

type fakeStore struct {
	runnerRegistrations map[string]RunnerRegistration
	workflows           map[string]map[string]Workflow
	runs                map[int64]Run
	logs                map[int64][]LogEntry
	nextRunID           int64
	nextLogID           int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		runnerRegistrations: make(map[string]RunnerRegistration),
		workflows:           make(map[string]map[string]Workflow),
		runs:                make(map[int64]Run),
		logs:                make(map[int64][]LogEntry),
	}
}

func (s *fakeStore) SaveRunnerRegistration(_ context.Context, registration RunnerRegistration) error {
	s.runnerRegistrations[registration.Name] = registration
	return nil
}

func (s *fakeStore) GetRunnerRegistration(_ context.Context, name string) (*RunnerRegistration, error) {
	registration, ok := s.runnerRegistrations[name]
	if !ok {
		return nil, ErrRunnerRegistrationNotFound
	}
	return &registration, nil
}

func (s *fakeStore) RemoveRunnerRegistration(_ context.Context, name string) error {
	delete(s.runnerRegistrations, name)
	return nil
}

func (s *fakeStore) UpsertWorkflow(_ context.Context, workflow Workflow) error {
	s.putWorkflow(nil, workflow)
	return nil
}

func (s *fakeStore) DeleteWorkflowsExcept(_ context.Context, repoName string, keep map[string]bool) error {
	for name := range s.workflows[repoName] {
		if !keep[name] {
			delete(s.workflows[repoName], name)
		}
	}
	return nil
}

func (s *fakeStore) ListWorkflowsByRepo(_ context.Context, repoName string) ([]Workflow, error) {
	repoWorkflows := s.workflows[repoName]
	workflows := make([]Workflow, 0, len(repoWorkflows))
	for _, workflow := range repoWorkflows {
		workflows = append(workflows, cloneWorkflow(workflow))
	}
	return workflows, nil
}

func (s *fakeStore) CreateRun(_ context.Context, run Run) (*Run, error) {
	return s.insertRun(nil, run), nil
}

func (s *fakeStore) GetRun(_ context.Context, id int64) (*Run, error) {
	run := s.findRun(id)
	if run == nil {
		return nil, ErrRunNotFound
	}
	return run, nil
}

func (s *fakeStore) UpdateRun(_ context.Context, run Run) error {
	s.updateRun(nil, run)
	return nil
}

func (s *fakeStore) ListRuns(_ context.Context) ([]Run, error) {
	runs := make([]Run, 0, len(s.runs))
	for _, run := range s.runs {
		runs = append(runs, cloneRun(run))
	}
	return runs, nil
}

func (s *fakeStore) CreateLogEntry(_ context.Context, entry LogEntry) error {
	s.putLogEntry(nil, entry)
	return nil
}

func (s *fakeStore) ListLogEntriesByRun(_ context.Context, runID int64) ([]LogEntry, error) {
	return s.logsForRun(runID), nil
}

func (s *fakeStore) DeleteRun(_ context.Context, runID int64) error {
	delete(s.runs, runID)
	delete(s.logs, runID)
	return nil
}

func (s *fakeStore) putRunnerRegistration(t *testing.T, registration RunnerRegistration) {
	if t != nil {
		t.Helper()
	}
	s.runnerRegistrations[registration.Name] = registration
}

func (s *fakeStore) putWorkflow(t *testing.T, workflow Workflow) {
	if t != nil {
		t.Helper()
	}
	if workflow.RepoName == "" {
		if t != nil {
			t.Fatal("workflow repo name is required")
		}
		return
	}
	if s.workflows[workflow.RepoName] == nil {
		s.workflows[workflow.RepoName] = make(map[string]Workflow)
	}
	s.workflows[workflow.RepoName][workflow.Name] = cloneWorkflow(workflow)
}

func (s *fakeStore) insertRun(t *testing.T, run Run) *Run {
	if t != nil {
		t.Helper()
	}
	if run.ID == 0 {
		s.nextRunID++
		run.ID = s.nextRunID
	}
	s.runs[run.ID] = cloneRun(run)
	inserted := cloneRun(run)
	return &inserted
}

func (s *fakeStore) getRun(t *testing.T, id int64) Run {
	t.Helper()
	run := s.findRun(id)
	if run == nil {
		t.Fatalf("run %d not found", id)
	}
	return *run
}

func (s *fakeStore) findRun(id int64) *Run {
	run, ok := s.runs[id]
	if !ok {
		return nil
	}
	cloned := cloneRun(run)
	return &cloned
}

func (s *fakeStore) updateRun(t *testing.T, run Run) {
	if t != nil {
		t.Helper()
	}
	s.runs[run.ID] = cloneRun(run)
}

func (s *fakeStore) runsForRepo(repoName string) []Run {
	var runs []Run
	for _, run := range s.runs {
		if run.RepoName == repoName {
			runs = append(runs, cloneRun(run))
		}
	}
	return runs
}

func (s *fakeStore) putLogEntry(t *testing.T, entry LogEntry) {
	if t != nil {
		t.Helper()
	}
	if entry.ID == 0 {
		s.nextLogID++
		entry.ID = s.nextLogID
	}
	s.logs[entry.RunID] = append(s.logs[entry.RunID], entry)
}

func (s *fakeStore) logsForRun(runID int64) []LogEntry {
	logs := make([]LogEntry, len(s.logs[runID]))
	copy(logs, s.logs[runID])
	return logs
}

func findWorkflow(t *testing.T, workflows []Workflow, name string) Workflow {
	t.Helper()
	workflow := findWorkflowMaybe(workflows, name)
	if workflow == nil {
		t.Fatalf("workflow %q not found in %#v", name, workflows)
	}
	return *workflow
}

func findWorkflowMaybe(workflows []Workflow, name string) *Workflow {
	for _, workflow := range workflows {
		if workflow.Name == name {
			cloned := cloneWorkflow(workflow)
			return &cloned
		}
	}
	return nil
}

func assertRunFailed(t *testing.T, run Run, reason FailureReason, finishedAt time.Time) {
	t.Helper()
	if run.Status != RunFailed {
		t.Fatalf("status = %q, want failed", run.Status)
	}
	if run.FinishedAt == nil || !run.FinishedAt.Equal(finishedAt) {
		t.Fatalf("finished_at = %v, want %s", run.FinishedAt, finishedAt)
	}
	if run.FailureReason == nil || *run.FailureReason != reason {
		t.Fatalf("failure_reason = %v, want %q", run.FailureReason, reason)
	}
}

func cloneWorkflow(workflow Workflow) Workflow {
	if workflow.Container != nil {
		container := *workflow.Container
		workflow.Container = &container
	}
	if workflow.Triggers != nil {
		triggers := make(map[EventType]bool, len(workflow.Triggers))
		for eventType, enabled := range workflow.Triggers {
			triggers[eventType] = enabled
		}
		workflow.Triggers = triggers
	}
	return workflow
}

// fakeRepoAccessChecker implements RepoAccessChecker for tests.
type fakeRepoAccessChecker struct {
	allowAll bool
}

func (c *fakeRepoAccessChecker) CanWriteToRepo(_ context.Context, username, repoName string) (bool, error) {
	if c.allowAll {
		return true, nil
	}
	// By default, deny unless the username is "writer" or "admin".
	if username == "writer" || username == "admin" {
		return true, nil
	}
	return false, nil
}

func cloneRun(run Run) Run {
	if run.Container != nil {
		container := *run.Container
		run.Container = &container
	}
	if run.StartedAt != nil {
		startedAt := *run.StartedAt
		run.StartedAt = &startedAt
	}
	if run.FinishedAt != nil {
		finishedAt := *run.FinishedAt
		run.FinishedAt = &finishedAt
	}
	if run.FailureReason != nil {
		failureReason := *run.FailureReason
		run.FailureReason = &failureReason
	}
	return run
}

func ptr[T any](value T) *T {
	return &value
}
