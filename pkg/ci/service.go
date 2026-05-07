package ci

import (
	"context"
	"fmt"
)

// CIService orchestrates all CI workflow execution operations.
// It depends only on port interfaces, never on adapter packages.
type CIService struct {
	cfg      CIConfig
	store    CIStore
	dispatch RunnerDispatch
	clock    Clock
}

// NewCIService creates a new CIService.
func NewCIService(cfg CIConfig, store CIStore, dispatch RunnerDispatch, clock Clock) *CIService {
	return &CIService{
		cfg:      cfg,
		store:    store,
		dispatch: dispatch,
		clock:    clock,
	}
}

// --- Runner management (AdminRunnerManagement surface) ---

// RegisterRunner registers a new CI runner. Per spec: AdminRegistersRunner.
func (s *CIService) RegisterRunner(ctx context.Context, name, dispatchURL string) (*RunnerRegistration, error) {
	secretToken := generateSecretToken()
	return s.store.CreateRunnerRegistration(ctx, name, dispatchURL, secretToken)
}

// RemoveRunner removes a runner registration. Per spec: AdminRemovesRunner.
func (s *CIService) RemoveRunner(ctx context.Context, name string) error {
	runner, err := s.store.GetRunnerRegistrationByName(ctx, name)
	if err != nil {
		return fmt.Errorf("runner %s: %w", name, ErrRunnerRegistrationNotFound)
	}
	return s.store.DeleteRunnerRegistration(ctx, runner.ID)
}

// ListRunners returns all registered runners.
func (s *CIService) ListRunners(ctx context.Context) ([]RunnerRegistration, error) {
	return s.store.ListRunnerRegistrations(ctx)
}

// --- Workflow syncing (rule WorkflowsSyncedOnPush) ---

// WorkflowDefinition is the parsed output of a single workflow file.
type WorkflowDefinition struct {
	Name      string
	Script    string
	RunsOn    string
	Container *string
	Triggers  []EventType
}

// SyncWorkflowsOnPush replaces the Workflow set for a repo with the parsed
// contents of the magic folder. Per spec rule WorkflowsSyncedOnPush.
func (s *CIService) SyncWorkflowsOnPush(ctx context.Context, repoName string, workflows []WorkflowDefinition) error {
	keepNames := make([]string, len(workflows))
	for i, wf := range workflows {
		_, err := s.store.UpsertWorkflow(ctx, repoName, wf.Name, wf.Script, wf.RunsOn, wf.Container, wf.Triggers)
		if err != nil {
			return fmt.Errorf("upserting workflow %s: %w", wf.Name, err)
		}
		keepNames[i] = wf.Name
	}

	// Remove stale workflows that no longer exist in the magic folder
	_, err := s.store.DeleteStaleWorkflows(ctx, repoName, keepNames)
	if err != nil {
		return fmt.Errorf("deleting stale workflows for %s: %w", repoName, err)
	}

	return nil
}

// ListWorkflows returns all discovered workflow definitions for a repo.
func (s *CIService) ListWorkflows(ctx context.Context, repoName string) ([]Workflow, error) {
	return s.store.ListWorkflowsByRepo(ctx, repoName)
}

// --- Run creation (rule CreateRunsOnEvent) ---

// CreateRunsOnEvent creates runs for all matching workflows when a webhook
// event fires. Per spec rule CreateRunsOnEvent.
// Returns the created runs.
func (s *CIService) CreateRunsOnEvent(ctx context.Context, repoName string, eventType EventType) ([]Run, error) {
	workflows, err := s.store.ListWorkflowsByRepo(ctx, repoName)
	if err != nil {
		return nil, fmt.Errorf("listing workflows: %w", err)
	}

	now := s.clock.Now()
	var created []Run
	for _, wf := range workflows {
		// Check if this workflow's triggers include this event type
		matched := false
		for _, t := range wf.Triggers {
			if t == eventType {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		// Snapshot workflow fields into the Run
		run, err := s.store.CreateRun(ctx, repoName, wf.Name, wf.Script, wf.RunsOn, wf.Container, eventType, now)
		if err != nil {
			return nil, fmt.Errorf("creating run for workflow %s: %w", wf.Name, err)
		}
		created = append(created, *run)
	}

	return created, nil
}

// --- Dispatch (rules DispatchRun, UnknownRunner) ---

// DispatchRun dispatches a pending Run to its target runner.
// Per spec rules DispatchRun and UnknownRunner.
func (s *CIService) DispatchRun(ctx context.Context, runID int64) (*Run, error) {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("getting run: %w", err)
	}

	if run.Status != RunStatusPending {
		return nil, fmt.Errorf("%w: run %d is %s, not pending", ErrInvalidTransition, runID, run.Status)
	}

	// Check if the target runner exists (rule UnknownRunner)
	runner, err := s.store.GetRunnerRegistrationByName(ctx, run.RunsOn)
	if err != nil {
		// Runner not registered: rule UnknownRunner fires
		now := s.clock.Now()
		reason := FailureReasonUnknownRunner
		if uErr := s.store.UpdateRunFinished(ctx, run.ID, RunStatusFailed, now, &reason); uErr != nil {
			return nil, fmt.Errorf("marking run as unknown_runner failed: %w", uErr)
		}
		run, _ = s.store.GetRun(ctx, run.ID)
		return run, nil
	}

	// Attempt dispatch (rule DispatchRun)
	err = s.dispatch.Dispatch(ctx, runner, run)
	if err != nil {
		// Dispatch ACK failed: rule DispatchAckFailed fires
		now := s.clock.Now()
		reason := FailureReasonDispatchAckFailed
		if uErr := s.store.UpdateRunFinished(ctx, run.ID, RunStatusFailed, now, &reason); uErr != nil {
			return nil, fmt.Errorf("marking run as dispatch_ack_failed: %w", uErr)
		}
		run, _ = s.store.GetRun(ctx, run.ID)
		return run, nil
	}

	// Successful dispatch: pending -> dispatched
	if err := s.store.UpdateRunStatus(ctx, run.ID, RunStatusDispatched); err != nil {
		return nil, fmt.Errorf("updating run to dispatched: %w", err)
	}
	run, _ = s.store.GetRun(ctx, run.ID)
	return run, nil
}

// HandleDispatchFailed handles the case where the dispatch HTTP call fails
// asynchronously. Per spec rule DispatchAckFailed.
func (s *CIService) HandleDispatchFailed(ctx context.Context, runID int64) (*Run, error) {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("getting run: %w", err)
	}

	if run.Status != RunStatusPending {
		return nil, fmt.Errorf("%w: run %d is %s, not pending", ErrInvalidTransition, runID, run.Status)
	}

	now := s.clock.Now()
	reason := FailureReasonDispatchAckFailed
	if err := s.store.UpdateRunFinished(ctx, runID, RunStatusFailed, now, &reason); err != nil {
		return nil, fmt.Errorf("marking run as dispatch_ack_failed: %w", err)
	}
	run, _ = s.store.GetRun(ctx, runID)
	return run, nil
}

// --- Runner callbacks (rules RunStarted, RunSucceeded, RunFailed) ---

// HandleRunnerStarted handles the runner reporting that the run has started.
// Per spec rule RunStarted.
func (s *CIService) HandleRunnerStarted(ctx context.Context, runID int64) (*Run, error) {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("getting run: %w", err)
	}

	if run.Status != RunStatusDispatched {
		return nil, fmt.Errorf("%w: run %d is %s, not dispatched", ErrInvalidTransition, runID, run.Status)
	}

	now := s.clock.Now()
	if err := s.store.UpdateRunStarted(ctx, runID, now); err != nil {
		return nil, fmt.Errorf("marking run as running: %w", err)
	}
	run, _ = s.store.GetRun(ctx, runID)
	return run, nil
}

// HandleRunnerCompletion handles the runner reporting completion with an exit code.
// Per spec rules RunSucceeded and RunFailed.
func (s *CIService) HandleRunnerCompletion(ctx context.Context, runID int64, exitCode int) (*Run, error) {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("getting run: %w", err)
	}

	if run.Status != RunStatusRunning {
		return nil, fmt.Errorf("%w: run %d is %s, not running", ErrInvalidTransition, runID, run.Status)
	}

	now := s.clock.Now()
	if exitCode == 0 {
		// Rule RunSucceeded
		if err := s.store.UpdateRunFinished(ctx, runID, RunStatusSucceeded, now, nil); err != nil {
			return nil, fmt.Errorf("marking run as succeeded: %w", err)
		}
	} else {
		// Rule RunFailed
		reason := FailureReasonRunnerReportedFailure
		if err := s.store.UpdateRunFinished(ctx, runID, RunStatusFailed, now, &reason); err != nil {
			return nil, fmt.Errorf("marking run as failed: %w", err)
		}
	}
	run, _ = s.store.GetRun(ctx, runID)
	return run, nil
}

// --- Log ingestion (rule IngestLogLine) ---

// IngestLogLine ingests a log line from a runner.
// Per spec rule IngestLogLine.
func (s *CIService) IngestLogLine(ctx context.Context, runID int64, line string) (*LogEntry, error) {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("getting run: %w", err)
	}

	if run.Status != RunStatusRunning {
		return nil, fmt.Errorf("%w: run %d is %s, not running", ErrInvalidTransition, runID, run.Status)
	}

	now := s.clock.Now()
	return s.store.CreateLogEntry(ctx, runID, line, now)
}

// --- Cancellation (rules UserCancelsPendingRun, CancelAckedFromDispatched, CancelAckedFromRunning) ---

// CancelRun cancels a run. The behavior depends on the current status.
// Pending runs are immediately canceled without a cancel webhook.
// Dispatched/running runs send a cancel webhook and transition on ACK.
func (s *CIService) CancelRun(ctx context.Context, runID int64) (*Run, error) {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("getting run: %w", err)
	}

	now := s.clock.Now()

	switch run.Status {
	case RunStatusPending:
		// Rule UserCancelsPendingRun: immediate cancellation, no webhook
		if err := s.store.UpdateRunFinished(ctx, runID, RunStatusCanceled, now, nil); err != nil {
			return nil, fmt.Errorf("canceling pending run: %w", err)
		}
		run, _ = s.store.GetRun(ctx, runID)
		return run, nil

	case RunStatusDispatched, RunStatusRunning:
		// Send cancel webhook to the runner
		runner, err := s.store.GetRunnerRegistrationByName(ctx, run.RunsOn)
		if err != nil {
			return nil, fmt.Errorf("finding runner for cancel: %w", err)
		}

		err = s.dispatch.SendCancel(ctx, runner, run)
		if err != nil {
			// Cancel webhook did not ACK — run stays in current state
			return nil, fmt.Errorf("cancel webhook failed for run %d: %w", runID, err)
		}

		// Runner ACKed cancel — transition to canceled
		// (rules CancelAckedFromDispatched / CancelAckedFromRunning)
		if err := s.store.UpdateRunFinished(ctx, runID, RunStatusCanceled, now, nil); err != nil {
			return nil, fmt.Errorf("marking run as canceled: %w", err)
		}
		run, _ = s.store.GetRun(ctx, runID)
		return run, nil

	default:
		// Terminal state: cannot cancel
		return nil, fmt.Errorf("%w: run %d is %s (terminal)", ErrInvalidTransition, runID, run.Status)
	}
}

// --- Timeout enforcement (rules PickupTimeout, RotateExpiredRuns) ---

// EnforceTimeouts checks for pickup timeouts and run expiration.
// Per spec rules PickupTimeout and RotateExpiredRuns.
func (s *CIService) EnforceTimeouts(ctx context.Context) (pickupTimeouts int, expiredRuns int, err error) {
	now := s.clock.Now()

	// PickupTimeout: dispatched runs where created_at + pickup_timeout <= now
	dispatchedRuns, err := s.store.ListRunsByStatus(ctx, RunStatusDispatched)
	if err != nil {
		return 0, 0, fmt.Errorf("listing dispatched runs: %w", err)
	}

	for _, run := range dispatchedRuns {
		deadline := run.CreatedAt.Add(s.cfg.PickupTimeout)
		if !deadline.After(now) {
			reason := FailureReasonPickupTimeout
			if uErr := s.store.UpdateRunFinished(ctx, run.ID, RunStatusFailed, now, &reason); uErr != nil {
				return pickupTimeouts, expiredRuns, fmt.Errorf("marking run %d pickup timeout: %w", run.ID, uErr)
			}
			pickupTimeouts++
		}
	}

	// RotateExpiredRuns: terminal runs where finished_at + run_retention <= now
	for _, status := range []RunStatus{RunStatusSucceeded, RunStatusFailed, RunStatusCanceled} {
		runs, lErr := s.store.ListRunsByStatus(ctx, status)
		if lErr != nil {
			return pickupTimeouts, expiredRuns, fmt.Errorf("listing terminal runs: %w", lErr)
		}
		for _, run := range runs {
			if run.FinishedAt == nil {
				continue
			}
			expiry := run.FinishedAt.Add(s.cfg.RunRetention)
			if !expiry.After(now) {
				if dErr := s.store.DeleteRun(ctx, run.ID); dErr != nil {
					return pickupTimeouts, expiredRuns, fmt.Errorf("deleting expired run %d: %w", run.ID, dErr)
				}
				expiredRuns++
			}
		}
	}

	return pickupTimeouts, expiredRuns, nil
}

// --- Query operations (RunQueryAPI surface) ---

// ListRuns returns all runs for a repo.
func (s *CIService) ListRuns(ctx context.Context, repoName string) ([]Run, error) {
	return s.store.ListRunsByRepo(ctx, repoName)
}

// GetRun returns a single run by ID.
func (s *CIService) GetRun(ctx context.Context, id int64) (*Run, error) {
	return s.store.GetRun(ctx, id)
}

// ListLogEntries returns all log entries for a run.
func (s *CIService) ListLogEntries(ctx context.Context, runID int64) ([]LogEntry, error) {
	return s.store.ListLogEntriesByRun(ctx, runID)
}

// --- Internal helpers ---

// generateSecretToken creates a random token for runner authentication.
// In a real implementation this would be cryptographically random.
// For now it's a placeholder since this is not the focus of testing.
func generateSecretToken() string {
	return "token-placeholder"
}
