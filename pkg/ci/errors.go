package ci

import "errors"

// Sentinel errors callers may match on with errors.Is. Adapters must
// translate their internal errors to these where the caller's contract
// requires it; never expose adapter- or driver-specific errors past
// the port.
var (
	// ErrNotAdmin is returned when an action requires admin role and
	// the caller does not have it.
	ErrNotAdmin = errors.New("ci: caller is not an admin")

	// ErrUnauthorizedRunner is returned when an inbound runner report
	// presents a token that does not match the runner assigned to the
	// run.
	ErrUnauthorizedRunner = errors.New("ci: runner token does not match assigned runner")

	// ErrInvalidTransition is returned when an operation requires a
	// run to be in a particular state and it is not.
	ErrInvalidTransition = errors.New("ci: invalid run state transition")

	// ErrWorkflowParse is returned when a workflow file in the magic
	// folder fails to parse. WorkflowSource adapters should wrap their
	// parse errors with this sentinel so callers can match on it.
	ErrWorkflowParse = errors.New("ci: workflow parse error")

	// ErrRunnerRegistrationNotFound is returned when looking up a
	// runner registration by name and no registration exists.
	ErrRunnerRegistrationNotFound = errors.New("ci: runner registration not found")

	// ErrRunNotFound is returned when looking up a run by ID and no
	// run exists.
	ErrRunNotFound = errors.New("ci: run not found")
)
