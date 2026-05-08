package ci

import (
	"context"
	"errors"
	"fmt"
	"io"

	"charm.land/log/v2"
)

// Service orchestrates the CI subsystem: runner registration, magic
// folder reconciliation, run lifecycle and retention. It is the
// composition target for the ports defined in ports.go and the only
// place rules from ci.allium are evaluated.
type Service struct {
	cfg            Config
	store          Store
	workflowSource WorkflowSource
	dispatcher     RunnerDispatcher
	tokens         TokenGenerator
	clock          Clock
	logger         *log.Logger
}

// NewService wires the service. logger may be nil; in that case the
// service writes log messages to a discard sink so callers do not
// need to construct a logger to use the service.
func NewService(cfg Config, store Store, ws WorkflowSource, dispatcher RunnerDispatcher, tokens TokenGenerator, clock Clock, logger *log.Logger) *Service {
	if logger == nil {
		logger = log.New(io.Discard)
	}
	return &Service{
		cfg:            cfg,
		store:          store,
		workflowSource: ws,
		dispatcher:     dispatcher,
		tokens:         tokens,
		clock:          clock,
		logger:         logger,
	}
}

// RegisterRunner records a runner registration on behalf of an admin
// and returns the registration including the freshly generated secret
// token. Non-admins receive ErrNotAdmin.
func (s *Service) RegisterRunner(ctx context.Context, user UserInfo, name, dispatchURL string) (RunnerRegistration, error) {
	if user.Role != "admin" {
		return RunnerRegistration{}, ErrNotAdmin
	}
	token, err := s.tokens.NewToken()
	if err != nil {
		return RunnerRegistration{}, fmt.Errorf("generate runner token: %w", err)
	}
	registration := RunnerRegistration{
		Name:        name,
		DispatchURL: dispatchURL,
		SecretToken: token,
	}
	if err := s.store.SaveRunnerRegistration(ctx, registration); err != nil {
		return RunnerRegistration{}, fmt.Errorf("save runner registration: %w", err)
	}
	return registration, nil
}

// RemoveRunner deletes a runner registration. It is admin-only.
// Removing a runner does not affect runs already dispatched to it.
func (s *Service) RemoveRunner(ctx context.Context, user UserInfo, name string) error {
	if user.Role != "admin" {
		return ErrNotAdmin
	}
	return s.store.RemoveRunnerRegistration(ctx, name)
}

// ValidateWorkflowsAtCommit parses the magic folder at the given
// commit SHA and returns the parse error (if any) without touching
// the stored workflow set. The push gate (surface RepoPushGate)
// calls this at pre-receive time against the incoming new tree so
// pushes with unparseable workflow files can be rejected before the
// ref is activated.
func (s *Service) ValidateWorkflowsAtCommit(ctx context.Context, repoName, commitSHA string) error {
	if _, err := s.workflowSource.ParseMagicFolderAtCommit(ctx, repoName, commitSHA); err != nil {
		return err
	}
	return nil
}

// SyncWorkflowsOnPush reconciles the stored Workflow set for repoName
// with the parsed contents of its magic folder. If the parse fails,
// the stored set is left untouched and the parse error is returned;
// the caller (the push gate) is expected to translate that into a
// push rejection.
func (s *Service) SyncWorkflowsOnPush(ctx context.Context, repoName string) error {
	defs, err := s.workflowSource.ParseMagicFolder(ctx, repoName)
	if err != nil {
		return err
	}

	keep := make(map[string]bool, len(defs))
	for _, def := range defs {
		workflow := Workflow{
			RepoName:  repoName,
			Name:      def.Name,
			Script:    def.Script,
			RunsOn:    def.RunsOn,
			Container: def.Container,
			Triggers:  def.Triggers,
		}
		if err := s.store.UpsertWorkflow(ctx, workflow); err != nil {
			return fmt.Errorf("upsert workflow %q: %w", def.Name, err)
		}
		keep[def.Name] = true
	}
	if err := s.store.DeleteWorkflowsExcept(ctx, repoName, keep); err != nil {
		return fmt.Errorf("delete stale workflows: %w", err)
	}
	return nil
}

// HandleWebhookEvent fans an inbound webhook event out into pending
// Runs, one per Workflow whose triggers include the event type. The
// trigger-time workflow definition is snapshotted into the Run.
func (s *Service) HandleWebhookEvent(ctx context.Context, repoName string, eventType EventType) error {
	workflows, err := s.store.ListWorkflowsByRepo(ctx, repoName)
	if err != nil {
		return fmt.Errorf("list workflows: %w", err)
	}
	now := s.clock.Now()
	for _, workflow := range workflows {
		if !workflow.Triggers[eventType] {
			continue
		}
		run := Run{
			RepoName:         repoName,
			WorkflowName:     workflow.Name,
			Script:           workflow.Script,
			RunsOn:           workflow.RunsOn,
			Container:        workflow.Container,
			TriggeredByEvent: eventType,
			Status:           RunPending,
			CreatedAt:        now,
		}
		if _, err := s.store.CreateRun(ctx, run); err != nil {
			return fmt.Errorf("create run for workflow %q: %w", workflow.Name, err)
		}
	}
	return nil
}

// DispatchPendingRun moves a pending run forward by either dispatching
// it to its assigned runner or marking it failed. The error return is
// reserved for unexpected I/O failures; expected failure modes
// (unknown runner, dispatch ack failure) are recorded on the Run
// itself and return nil so callers can advance to the next run.
func (s *Service) DispatchPendingRun(ctx context.Context, runID int64) error {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("get run %d: %w", runID, err)
	}
	if run.Status != RunPending {
		return nil
	}

	registration, err := s.store.GetRunnerRegistration(ctx, run.RunsOn)
	if errors.Is(err, ErrRunnerRegistrationNotFound) {
		return s.failRun(ctx, run, FailureReasonUnknownRunner)
	}
	if err != nil {
		return fmt.Errorf("get runner registration %q: %w", run.RunsOn, err)
	}

	if err := s.dispatcher.DispatchRun(ctx, *registration, *run); err != nil {
		s.logger.Warn("dispatch ack failed", "run", run.ID, "runner", run.RunsOn, "err", err)
		return s.failRun(ctx, run, FailureReasonDispatchAckFailed)
	}

	run.Status = RunDispatched
	if err := s.store.UpdateRun(ctx, *run); err != nil {
		return fmt.Errorf("update dispatched run: %w", err)
	}
	return nil
}

// ReportStarted moves a dispatched run to running. The runner must
// authenticate with the secret token issued at registration.
func (s *Service) ReportStarted(ctx context.Context, runnerToken string, runID int64) error {
	run, err := s.authorizeRunnerReport(ctx, runnerToken, runID)
	if err != nil {
		return err
	}
	if !run.CanTransition(RunRunning) {
		return ErrInvalidTransition
	}
	now := s.clock.Now()
	run.Status = RunRunning
	run.StartedAt = &now
	if err := s.store.UpdateRun(ctx, *run); err != nil {
		return fmt.Errorf("update started run: %w", err)
	}
	return nil
}

// ReportCompletion is the runner's report that a script finished.
// exit_code zero is success; non-zero is a runner-reported failure.
func (s *Service) ReportCompletion(ctx context.Context, runnerToken string, runID int64, exitCode int) error {
	run, err := s.authorizeRunnerReport(ctx, runnerToken, runID)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	if exitCode == 0 {
		if !run.CanTransition(RunSucceeded) {
			return ErrInvalidTransition
		}
		run.Status = RunSucceeded
		run.FinishedAt = &now
	} else {
		if !run.CanTransition(RunFailed) {
			return ErrInvalidTransition
		}
		reason := FailureReasonRunnerReportedFailure
		run.Status = RunFailed
		run.FinishedAt = &now
		run.FailureReason = &reason
	}
	if err := s.store.UpdateRun(ctx, *run); err != nil {
		return fmt.Errorf("update completed run: %w", err)
	}
	return nil
}

// IngestLogLine appends one line of runner output to a running run.
// Logs from non-running runs are rejected with ErrInvalidTransition;
// in particular, runs that have already terminated do not accept
// further log entries (matching the cancel semantics in ci.allium).
func (s *Service) IngestLogLine(ctx context.Context, runnerToken string, runID int64, line string) error {
	run, err := s.authorizeRunnerReport(ctx, runnerToken, runID)
	if err != nil {
		return err
	}
	if run.Status != RunRunning {
		return ErrInvalidTransition
	}
	entry := LogEntry{
		RunID:      run.ID,
		Line:       line,
		ReceivedAt: s.clock.Now(),
	}
	if err := s.store.CreateLogEntry(ctx, entry); err != nil {
		return fmt.Errorf("create log entry: %w", err)
	}
	return nil
}

// CancelRun terminates a run. Pending runs are canceled locally with
// no runner contact. Dispatched and running runs require a successful
// cancel ACK from the runner before flipping to canceled; if the
// runner does not ACK, the dispatcher error is returned and the run
// keeps its current state so the caller may retry.
func (s *Service) CancelRun(ctx context.Context, _ UserInfo, runID int64) error {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("get run %d: %w", runID, err)
	}

	switch run.Status {
	case RunPending:
		return s.markCanceled(ctx, run)
	case RunDispatched, RunRunning:
		registration, err := s.store.GetRunnerRegistration(ctx, run.RunsOn)
		if err != nil {
			return fmt.Errorf("get runner registration %q: %w", run.RunsOn, err)
		}
		if err := s.dispatcher.CancelRun(ctx, *registration, *run); err != nil {
			return err
		}
		return s.markCanceled(ctx, run)
	default:
		return ErrInvalidTransition
	}
}

// ListAllRuns returns every stored run. Used by the read-only
// RunQueryAPI surface; per-repo filtering can layer on top.
func (s *Service) ListAllRuns(ctx context.Context) ([]Run, error) {
	return s.store.ListRuns(ctx)
}

// GetRun returns the run with the given ID, or ErrRunNotFound.
// Used by the read-only RunQueryAPI surface.
func (s *Service) GetRun(ctx context.Context, id int64) (Run, error) {
	run, err := s.store.GetRun(ctx, id)
	if err != nil {
		return Run{}, err
	}
	return *run, nil
}

// ListLogEntries returns the log entries appended to a run, in
// insertion order.
func (s *Service) ListLogEntries(ctx context.Context, runID int64) ([]LogEntry, error) {
	return s.store.ListLogEntriesByRun(ctx, runID)
}

// ListWorkflowsByRepo returns the stored Workflow set for a repo.
func (s *Service) ListWorkflowsByRepo(ctx context.Context, repoName string) ([]Workflow, error) {
	return s.store.ListWorkflowsByRepo(ctx, repoName)
}

// ListPendingRuns returns the runs in the RunPending state.
// Background loops use it to drive DispatchPendingRun for each
// freshly created Run. The filter is performed in-process; if and
// when scale demands it the Store port can grow a status-aware
// list method.
func (s *Service) ListPendingRuns(ctx context.Context) ([]Run, error) {
	all, err := s.store.ListRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	pending := make([]Run, 0, len(all))
	for _, run := range all {
		if run.Status == RunPending {
			pending = append(pending, run)
		}
	}
	return pending, nil
}

// EnforceTimeouts fires the PickupTimeout rule across all currently
// dispatched runs. Idempotent: terminal runs are ignored, so it is
// safe to call on a schedule.
func (s *Service) EnforceTimeouts(ctx context.Context) error {
	runs, err := s.store.ListRuns(ctx)
	if err != nil {
		return fmt.Errorf("list runs: %w", err)
	}
	now := s.clock.Now()
	for i := range runs {
		run := runs[i]
		if run.Status != RunDispatched {
			continue
		}
		deadline := run.CreatedAt.Add(s.cfg.PickupTimeout)
		if now.Before(deadline) {
			continue
		}
		if err := s.failRun(ctx, &run, FailureReasonPickupTimeout); err != nil {
			return err
		}
	}
	return nil
}

// RotateExpiredRuns removes terminal runs (and their log entries, via
// the store's DeleteRun cascade) whose retention window has elapsed.
func (s *Service) RotateExpiredRuns(ctx context.Context) error {
	runs, err := s.store.ListRuns(ctx)
	if err != nil {
		return fmt.Errorf("list runs: %w", err)
	}
	now := s.clock.Now()
	for _, run := range runs {
		if !run.Status.IsTerminal() || run.FinishedAt == nil {
			continue
		}
		deadline := run.FinishedAt.Add(s.cfg.RunRetention)
		if now.Before(deadline) {
			continue
		}
		if err := s.store.DeleteRun(ctx, run.ID); err != nil {
			return fmt.Errorf("delete run %d: %w", run.ID, err)
		}
	}
	return nil
}

// authorizeRunnerReport loads the run and verifies the inbound
// runner-supplied token matches the runner assigned to that run.
// Mismatched tokens get ErrUnauthorizedRunner so the caller can
// translate to HTTP 401/403 without leaking which arm failed.
func (s *Service) authorizeRunnerReport(ctx context.Context, runnerToken string, runID int64) (*Run, error) {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("get run %d: %w", runID, err)
	}
	registration, err := s.store.GetRunnerRegistration(ctx, run.RunsOn)
	if err != nil {
		if errors.Is(err, ErrRunnerRegistrationNotFound) {
			return nil, ErrUnauthorizedRunner
		}
		return nil, fmt.Errorf("get runner registration %q: %w", run.RunsOn, err)
	}
	if registration.SecretToken != runnerToken {
		return nil, ErrUnauthorizedRunner
	}
	return run, nil
}

// failRun is the shared terminal-failure transition: set status,
// finished_at and failure_reason, persist. Caller has already
// established that the failure path applies.
func (s *Service) failRun(ctx context.Context, run *Run, reason FailureReason) error {
	now := s.clock.Now()
	run.Status = RunFailed
	run.FinishedAt = &now
	r := reason
	run.FailureReason = &r
	if err := s.store.UpdateRun(ctx, *run); err != nil {
		return fmt.Errorf("update failed run: %w", err)
	}
	return nil
}

// markCanceled is the shared terminal-cancel transition. The state
// machine guards have already been checked by the caller.
func (s *Service) markCanceled(ctx context.Context, run *Run) error {
	now := s.clock.Now()
	run.Status = RunCanceled
	run.FinishedAt = &now
	if err := s.store.UpdateRun(ctx, *run); err != nil {
		return fmt.Errorf("update canceled run: %w", err)
	}
	return nil
}
