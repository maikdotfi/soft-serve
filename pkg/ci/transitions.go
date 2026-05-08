package ci

// allowedRunTransitions encodes the Run.status edges declared in the
// `transitions status { ... }` block of ci.allium. Any edge not in
// this map is rejected. Terminal states have no entry; they cannot
// transition out.
var allowedRunTransitions = map[RunStatus]map[RunStatus]bool{
	RunPending: {
		RunDispatched: true,
		RunFailed:     true,
		RunCanceled:   true,
	},
	RunDispatched: {
		RunRunning:  true,
		RunFailed:   true,
		RunCanceled: true,
	},
	RunRunning: {
		RunSucceeded: true,
		RunFailed:    true,
		RunCanceled:  true,
	},
}

// terminalRunStatuses is the closed set of statuses with no outbound
// edges. Mirrors the `terminal:` line in the ci.allium transitions
// block.
var terminalRunStatuses = map[RunStatus]bool{
	RunSucceeded: true,
	RunFailed:    true,
	RunCanceled:  true,
}

// IsTerminal reports whether s is a terminal status (no outbound
// edges in the run state machine).
func (s RunStatus) IsTerminal() bool {
	return terminalRunStatuses[s]
}

// CanTransition reports whether the run's current status may
// transition to next under the rules declared in ci.allium.
func (r *Run) CanTransition(next RunStatus) bool {
	return allowedRunTransitions[r.Status][next]
}
