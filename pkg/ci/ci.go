// Package ci implements the CI workflow execution domain for Soft Serve.
// The spec is defined in ci.allium at the repository root.
//
// Architecture follows AGENTS.md: domain types and port interfaces live here;
// adapters (runner dispatch, database) live in pkg/ci/adapters/.
// The domain package never imports adapter packages.
package ci

import (
	"context"
	"errors"
	"time"
)

// --- Config ---

// CIConfig holds all configuration parameters for the CI subsystem.
// Corresponds to the config block in ci.allium.
type CIConfig struct {
	PickupTimeout time.Duration // default: 1 hour
	RunRetention  time.Duration // default: 7 days
}

// DefaultCIConfig returns the config with defaults specified in the spec.
func DefaultCIConfig() CIConfig {
	return CIConfig{
		PickupTimeout: 1 * time.Hour,
		RunRetention:  7 * 24 * time.Hour,
	}
}

// --- EventType enum ---

// EventType represents the webhook event types Soft Serve exposes.
// Spec: enum EventType { push | branch_tag_create | branch_tag_delete | collaborator | repository | repository_visibility_change }
type EventType string

const (
	EventTypePush                     EventType = "push"
	EventTypeBranchTagCreate          EventType = "branch_tag_create"
	EventTypeBranchTagDelete          EventType = "branch_tag_delete"
	EventTypeCollaborator             EventType = "collaborator"
	EventTypeRepository               EventType = "repository"
	EventTypeRepositoryVisibilityChange EventType = "repository_visibility_change"
)

// ValidEventTypes is the set of valid EventType values.
var ValidEventTypes = map[EventType]bool{
	EventTypePush:                     true,
	EventTypeBranchTagCreate:          true,
	EventTypeBranchTagDelete:          true,
	EventTypeCollaborator:             true,
	EventTypeRepository:               true,
	EventTypeRepositoryVisibilityChange: true,
}

// --- FailureReason enum ---

// FailureReason represents why a Run ended in the failed terminal state.
// Spec: enum FailureReason { dispatch_ack_failed | pickup_timeout | runner_reported_failure | unknown_runner }
type FailureReason string

const (
	FailureReasonDispatchAckFailed    FailureReason = "dispatch_ack_failed"
	FailureReasonPickupTimeout        FailureReason = "pickup_timeout"
	FailureReasonRunnerReportedFailure FailureReason = "runner_reported_failure"
	FailureReasonUnknownRunner        FailureReason = "unknown_runner"
)

// ValidFailureReasons is the set of valid FailureReason values.
var ValidFailureReasons = map[FailureReason]bool{
	FailureReasonDispatchAckFailed:    true,
	FailureReasonPickupTimeout:        true,
	FailureReasonRunnerReportedFailure: true,
	FailureReasonUnknownRunner:        true,
}

// --- Run status enum ---

// RunStatus represents the status of a Run.
// Spec: status: pending | dispatched | running | succeeded | failed | canceled
type RunStatus string

const (
	RunStatusPending    RunStatus = "pending"
	RunStatusDispatched RunStatus = "dispatched"
	RunStatusRunning    RunStatus = "running"
	RunStatusSucceeded  RunStatus = "succeeded"
	RunStatusFailed     RunStatus = "failed"
	RunStatusCanceled   RunStatus = "canceled"
)

// ValidRunStatuses is the set of valid RunStatus values.
var ValidRunStatuses = map[RunStatus]bool{
	RunStatusPending:    true,
	RunStatusDispatched: true,
	RunStatusRunning:    true,
	RunStatusSucceeded:  true,
	RunStatusFailed:     true,
	RunStatusCanceled:   true,
}

// --- Transition graph for Run.status ---

// RunTransitions defines the valid transitions for Run.status.
// Spec:
//
//	transitions status {
//	  pending    -> dispatched
//	  pending    -> failed         -- dispatch ACK failed
//	  pending    -> canceled       -- canceled before dispatch
//	  dispatched -> failed         -- pickup timeout
//	  dispatched -> running
//	  dispatched -> canceled       -- canceled after dispatch (runner ACKed cancel)
//	  running    -> succeeded
//	  running    -> failed
//	  running    -> canceled       -- canceled while running (runner ACKed cancel)
//	  terminal: succeeded, failed, canceled
//	}
var RunTransitions = map[RunStatus][]RunStatus{
	RunStatusPending:    {RunStatusDispatched, RunStatusFailed, RunStatusCanceled},
	RunStatusDispatched: {RunStatusRunning, RunStatusFailed, RunStatusCanceled},
	RunStatusRunning:    {RunStatusSucceeded, RunStatusFailed, RunStatusCanceled},
}

// RunTerminalStates defines terminal states with no outbound transitions.
var RunTerminalStates = map[RunStatus]bool{
	RunStatusSucceeded: true,
	RunStatusFailed:    true,
	RunStatusCanceled:  true,
}

// NonTerminalRunStatuses returns all non-terminal run statuses.
var NonTerminalRunStatuses = []RunStatus{RunStatusPending, RunStatusDispatched, RunStatusRunning}

// --- Entities ---

// RunnerRegistration represents a registered CI runner.
// Spec: entity RunnerRegistration { name: String, dispatch_url: String, secret_token: String }
type RunnerRegistration struct {
	ID          int64
	Name        string
	DispatchURL string
	SecretToken string
}

// Workflow represents a discovered workflow definition from the magic folder.
// Spec: entity Workflow { repo: Repo, name: String, script: String, runs_on: String, container: String?, triggers: Set<EventType> }
type Workflow struct {
	ID        int64
	RepoName  string
	Name      string
	Script    string
	RunsOn    string
	Container *string // optional
	Triggers  []EventType
}

// Run represents a single CI run execution.
// Spec: entity Run { ... }
type Run struct {
	ID       int64
	RepoName string

	// Snapshot of Workflow definition at trigger time
	WorkflowName string
	Script       string
	RunsOn       string
	Container    *string // optional

	TriggeredByEvent EventType
	Status           RunStatus
	CreatedAt        time.Time
	StartedAt        *time.Time   // set when status becomes running
	FinishedAt       *time.Time   // set when status becomes terminal
	FailureReason    *FailureReason // set when status becomes failed
}

// LogEntry represents a single log line from a runner.
// Spec: entity LogEntry { run: Run, line: String, received_at: Timestamp }
type LogEntry struct {
	ID         int64
	RunID      int64
	Line       string
	ReceivedAt time.Time
}

// --- State-dependent field helpers ---

// HasStartedAt returns whether the Run status requires started_at to be present.
// Per spec: started_at is set when status becomes running.
func HasStartedAt(s RunStatus) bool {
	switch s {
	case RunStatusRunning, RunStatusSucceeded, RunStatusFailed, RunStatusCanceled:
		return true
	default:
		return false
	}
}

// HasFinishedAt returns whether the Run status requires finished_at to be present.
// Per spec: finished_at is set when status becomes terminal (succeeded, failed, canceled).
func HasFinishedAt(s RunStatus) bool {
	return IsTerminal(s)
}

// HasFailureReason returns whether the Run status requires failure_reason to be present.
// Per spec: failure_reason is set when status becomes failed.
func HasFailureReason(s RunStatus) bool {
	return s == RunStatusFailed
}

// --- Transition helpers ---

// CanTransition checks whether a transition from src to dst is valid.
func CanTransition(src, dst RunStatus) bool {
	for _, s := range RunTransitions[src] {
		if s == dst {
			return true
		}
	}
	return false
}

// IsTerminal returns whether the given RunStatus is terminal.
func IsTerminal(s RunStatus) bool {
	return RunTerminalStates[s]
}

// --- Domain errors ---

// Sentinel errors for the CI domain. Adapters must translate to these.
var (
	ErrRunNotFound               = errors.New("run not found")
	ErrRunnerRegistrationNotFound = errors.New("runner registration not found")
	ErrWorkflowNotFound           = errors.New("workflow not found")
	ErrLogEntryNotFound           = errors.New("log entry not found")
	ErrInvalidTransition          = errors.New("invalid status transition")
	ErrRunnerNotFound             = errors.New("runner not found")
	ErrDispatchFailed             = errors.New("dispatch failed")
	ErrNotImplemented             = errors.New("not implemented")
)

// --- Port interfaces ---
// Per AGENTS.md: all external dependencies go through ports defined in the domain.

// Clock provides time injection for temporal tests.
type Clock interface {
	Now() time.Time
}

// WallClock is the default Clock using wall-clock time.
type WallClock struct{}

func (WallClock) Now() time.Time { return time.Now() }

// CIStore is the port interface for persisting CI domain entities.
type CIStore interface {
	// RunnerRegistration operations
	CreateRunnerRegistration(ctx context.Context, name, dispatchURL, secretToken string) (*RunnerRegistration, error)
	GetRunnerRegistration(ctx context.Context, id int64) (*RunnerRegistration, error)
	GetRunnerRegistrationByName(ctx context.Context, name string) (*RunnerRegistration, error)
	ListRunnerRegistrations(ctx context.Context) ([]RunnerRegistration, error)
	DeleteRunnerRegistration(ctx context.Context, id int64) error

	// Workflow operations
	UpsertWorkflow(ctx context.Context, repoName, name, script, runsOn string, container *string, triggers []EventType) (*Workflow, error)
	ListWorkflowsByRepo(ctx context.Context, repoName string) ([]Workflow, error)
	DeleteStaleWorkflows(ctx context.Context, repoName string, keepNames []string) (int64, error)

	// Run operations
	CreateRun(ctx context.Context, repoName, workflowName, script, runsOn string, container *string, triggeredByEvent EventType, createdAt time.Time) (*Run, error)
	GetRun(ctx context.Context, id int64) (*Run, error)
	ListRunsByRepo(ctx context.Context, repoName string) ([]Run, error)
	ListRunsByStatus(ctx context.Context, status RunStatus) ([]Run, error)
	UpdateRunStatus(ctx context.Context, id int64, status RunStatus) error
	UpdateRunStarted(ctx context.Context, id int64, startedAt time.Time) error
	UpdateRunFinished(ctx context.Context, id int64, status RunStatus, finishedAt time.Time, failureReason *FailureReason) error
	DeleteRun(ctx context.Context, id int64) error

	// LogEntry operations
	CreateLogEntry(ctx context.Context, runID int64, line string, receivedAt time.Time) (*LogEntry, error)
	ListLogEntriesByRun(ctx context.Context, runID int64) ([]LogEntry, error)
}

// RunnerDispatch is the port interface for communicating with external runners.
// It handles both outbound dispatch/cancel webhooks and provides a way for
// tests to simulate runner responses.
type RunnerDispatch interface {
	// Dispatch sends a dispatch webhook to the runner. Returns nil on
	// successful ACK (HTTP 2xx), or an error if the dispatch failed.
	Dispatch(ctx context.Context, runner *RunnerRegistration, run *Run) error

	// SendCancel sends a cancel webhook to the runner. Returns nil on
	// successful ACK (HTTP 2xx), or an error if the cancel failed.
	SendCancel(ctx context.Context, runner *RunnerRegistration, run *Run) error
}
