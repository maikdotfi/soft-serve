package ci

import (
	"context"
	"time"
)

// Store is the persistence port for the CI subsystem. Adapters
// translate driver errors to the sentinels defined in errors.go.
type Store interface {
	// SaveRunnerRegistration creates or replaces a runner
	// registration keyed by Name.
	SaveRunnerRegistration(ctx context.Context, registration RunnerRegistration) error

	// GetRunnerRegistration returns the registration with the given
	// name, or ErrRunnerRegistrationNotFound.
	GetRunnerRegistration(ctx context.Context, name string) (*RunnerRegistration, error)

	// RemoveRunnerRegistration removes a registration by name. It is
	// a no-op if no registration exists with that name.
	RemoveRunnerRegistration(ctx context.Context, name string) error

	// UpsertWorkflow creates or replaces a Workflow keyed by
	// (RepoName, Name).
	UpsertWorkflow(ctx context.Context, workflow Workflow) error

	// DeleteWorkflowsExcept removes every workflow for the repo
	// whose name is not in the keep set.
	DeleteWorkflowsExcept(ctx context.Context, repoName string, keep map[string]bool) error

	// ListWorkflowsByRepo returns the workflows stored for the given
	// repo. The returned slice is independent of the store.
	ListWorkflowsByRepo(ctx context.Context, repoName string) ([]Workflow, error)

	// CreateRun inserts a new Run and returns the stored row with
	// ID populated.
	CreateRun(ctx context.Context, run Run) (*Run, error)

	// GetRun returns the run with the given ID, or ErrRunNotFound.
	GetRun(ctx context.Context, id int64) (*Run, error)

	// UpdateRun replaces the stored row with the given Run, keyed by
	// ID.
	UpdateRun(ctx context.Context, run Run) error

	// ListRuns returns every stored run. Used by the periodic
	// reconciliation rules (timeouts, retention) which scan all runs.
	ListRuns(ctx context.Context) ([]Run, error)

	// CreateLogEntry inserts a new log entry for a run.
	CreateLogEntry(ctx context.Context, entry LogEntry) error

	// ListLogEntriesByRun returns the log entries for a run, in
	// insertion order.
	ListLogEntriesByRun(ctx context.Context, runID int64) ([]LogEntry, error)

	// DeleteRun removes a run and cascades to its log entries.
	DeleteRun(ctx context.Context, runID int64) error
}

// WorkflowSource parses the magic folder of a repository into
// WorkflowDefinitions. Adapters wrap their parse errors with
// ErrWorkflowParse so callers can match on it.
//
// ParseMagicFolder reads the repository's current HEAD tree; it is
// used for post-push reconciliation (rule WorkflowsSyncedOnPush).
//
// ParseMagicFolderAtCommit reads the tree at a specific commit SHA;
// it is used at pre-receive time, before the new ref has been
// activated, so the gate (surface RepoPushGate) can validate the
// incoming workflow files and reject pushes whose magic folder fails
// to parse.
type WorkflowSource interface {
	ParseMagicFolder(ctx context.Context, repoName string) ([]WorkflowDefinition, error)
	ParseMagicFolderAtCommit(ctx context.Context, repoName string, commitSHA string) ([]WorkflowDefinition, error)
}

// RunnerDispatcher sends dispatch and cancel webhooks to a runner.
// The dispatch webhook carries the snapshotted Run; the cancel
// webhook references the run by ID. A non-nil error means the runner
// did not ACK.
type RunnerDispatcher interface {
	DispatchRun(ctx context.Context, registration RunnerRegistration, run Run) error
	CancelRun(ctx context.Context, registration RunnerRegistration, run Run) error
}

// TokenGenerator produces opaque, unguessable strings used as runner
// secret tokens. The real adapter uses crypto/rand; tests use a
// fake that returns canned values.
type TokenGenerator interface {
	NewToken() (string, error)
}

// RepoAccessChecker checks whether a user has write access to a
// repository. The CI domain uses this to enforce per-repo ACLs on
// run-control operations such as cancellation.
//
// Adapters should return (false, nil) when the user does not have
// write access; a non-nil error indicates the check itself failed.
type RepoAccessChecker interface {
	CanWriteToRepo(ctx context.Context, username, repoName string) (bool, error)
}

// Clock is the time source for the service. Treated as an external
// service per the project's ports + adapters rule.
type Clock interface {
	Now() time.Time
}
