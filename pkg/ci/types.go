// Package ci is the CI workflow execution subsystem for Soft Serve.
//
// The package is the domain layer described by ci.allium: it owns the
// types, the run-state machine, and the Service that orchestrates
// runner registration, workflow synchronisation, run lifecycle and
// retention. It does not perform I/O. External services (storage,
// runner dispatch, workflow file parsing, time, randomness) are
// reached through ports defined here and implemented under
// pkg/ci/adapters/.
package ci

import "time"

// Config holds tunable timings for the CI subsystem.
type Config struct {
	// PickupTimeout is the maximum time from a Run being created to the
	// runner reporting started. After this, the run is failed with
	// FailureReasonPickupTimeout.
	PickupTimeout time.Duration

	// RunRetention is how long terminal Runs and their LogEntries are
	// kept after FinishedAt. After this, RotateExpiredRuns removes them.
	RunRetention time.Duration
}

// DefaultConfig returns the defaults from ci.allium.
func DefaultConfig() Config {
	return Config{
		PickupTimeout: time.Hour,
		RunRetention:  7 * 24 * time.Hour,
	}
}

// EventType is a webhook event that can trigger a workflow.
type EventType string

// EventType values mirror the ci.allium EventType enum.
const (
	EventTypePush                       EventType = "push"
	EventTypeBranchTagCreate            EventType = "branch_tag_create"
	EventTypeBranchTagDelete            EventType = "branch_tag_delete"
	EventTypeCollaborator               EventType = "collaborator"
	EventTypeRepository                 EventType = "repository"
	EventTypeRepositoryVisibilityChange EventType = "repository_visibility_change"
)

// ValidEventTypes is the closed set of EventType values. Use this to
// validate values arriving from outside the package.
var ValidEventTypes = map[EventType]bool{
	EventTypePush:                       true,
	EventTypeBranchTagCreate:            true,
	EventTypeBranchTagDelete:            true,
	EventTypeCollaborator:               true,
	EventTypeRepository:                 true,
	EventTypeRepositoryVisibilityChange: true,
}

// FailureReason explains why a Run ended in the failed state.
type FailureReason string

// FailureReason values mirror the ci.allium FailureReason enum.
const (
	FailureReasonDispatchAckFailed     FailureReason = "dispatch_ack_failed"
	FailureReasonPickupTimeout         FailureReason = "pickup_timeout"
	FailureReasonRunnerReportedFailure FailureReason = "runner_reported_failure"
	FailureReasonUnknownRunner         FailureReason = "unknown_runner"
)

// ValidFailureReasons is the closed set of FailureReason values.
var ValidFailureReasons = map[FailureReason]bool{
	FailureReasonDispatchAckFailed:     true,
	FailureReasonPickupTimeout:         true,
	FailureReasonRunnerReportedFailure: true,
	FailureReasonUnknownRunner:         true,
}

// RunStatus is the lifecycle state of a Run.
type RunStatus string

// RunStatus values mirror the ci.allium Run.status enum.
const (
	RunPending    RunStatus = "pending"
	RunDispatched RunStatus = "dispatched"
	RunRunning    RunStatus = "running"
	RunSucceeded  RunStatus = "succeeded"
	RunFailed     RunStatus = "failed"
	RunCanceled   RunStatus = "canceled"
)

// ValidRunStatuses is the closed set of RunStatus values.
var ValidRunStatuses = map[RunStatus]bool{
	RunPending:    true,
	RunDispatched: true,
	RunRunning:    true,
	RunSucceeded:  true,
	RunFailed:     true,
	RunCanceled:   true,
}

// RepoInfo identifies a repository at the boundary of the CI domain.
// The CI package does not own repository state; callers pass the name
// of an existing repo.
type RepoInfo struct {
	Name string
}

// UserInfo identifies a user at the boundary of the CI domain. Role
// distinguishes admins (who may register runners) from other users.
// Username is the user's login name, used for repo access checks.
type UserInfo struct {
	Role     string
	Username string
}

// RunnerRegistration is a runner the admin has registered with the
// server. Soft Serve dispatches runs to DispatchURL and authenticates
// inbound runner reports against SecretToken.
type RunnerRegistration struct {
	Name        string
	DispatchURL string
	SecretToken string
}

// WorkflowDefinition is the parsed contents of one workflow file in
// the magic folder of a repository, before it is associated with a
// repo and stored as a Workflow.
type WorkflowDefinition struct {
	Name      string
	Script    string
	RunsOn    string
	Container *string
	Triggers  map[EventType]bool
}

// Workflow is a stored workflow definition for a specific repository.
type Workflow struct {
	RepoName  string
	Name      string
	Script    string
	RunsOn    string
	Container *string
	Triggers  map[EventType]bool
}

// Run is a single execution of a Workflow. The script, runs_on and
// container fields are snapshotted at creation time so a later edit of
// the source workflow does not change what the Run executes.
type Run struct {
	ID               int64
	RepoName         string
	WorkflowName     string
	Script           string
	RunsOn           string
	Container        *string
	TriggeredByEvent EventType
	Status           RunStatus
	CreatedAt        time.Time
	StartedAt        *time.Time
	FinishedAt       *time.Time
	FailureReason    *FailureReason
}

// LogEntry is one line of runner-supplied log output for a Run.
type LogEntry struct {
	ID         int64
	RunID      int64
	Line       string
	ReceivedAt time.Time
}
