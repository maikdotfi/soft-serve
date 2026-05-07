// Package fake provides in-memory fake implementations of the CI port interfaces.
// Per AGENTS.md: this is the reference implementation used by tests across packages
// to lock in the port's contract.
package fake

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/ci"
)

// --- Fake Clock ---

// FakeClock is a controllable clock for temporal tests.
type FakeClock struct {
	mu  sync.RWMutex
	now time.Time
}

// NewFakeClock creates a new FakeClock starting at a fixed time.
func NewFakeClock() *FakeClock {
	return &FakeClock{now: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
}

// Now returns the current fake time.
func (c *FakeClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

// Advance moves the fake time forward by d.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Set sets the fake time to a specific value.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// --- Fake CIStore ---

// FakeCIStore is an in-memory implementation of ci.CIStore.
type FakeCIStore struct {
	mu                   sync.RWMutex
	runners             map[int64]*ci.RunnerRegistration
	runnersByName       map[string]int64
	workflows           map[int64]*ci.Workflow
	runs                map[int64]*ci.Run
	logEntries          map[int64]*ci.LogEntry
	nextID              int64

	// Error injection for testing error paths
	CreateRunnerErr        error
	GetRunnerErr           error
	ListRunnersErr         error
	DeleteRunnerErr        error
	UpsertWorkflowErr      error
	ListWorkflowsErr       error
	DeleteStaleWorkflowsErr error
	CreateRunErr           error
	GetRunErr              error
	ListRunsErr            error
	UpdateRunStatusErr     error
	UpdateRunStartedErr    error
	UpdateRunFinishedErr   error
	DeleteRunErr           error
	CreateLogEntryErr      error
	ListLogEntriesErr      error
}

// NewFakeCIStore creates a new FakeCIStore.
func NewFakeCIStore() *FakeCIStore {
	return &FakeCIStore{
		runners:       make(map[int64]*ci.RunnerRegistration),
		runnersByName: make(map[string]int64),
		workflows:     make(map[int64]*ci.Workflow),
		runs:          make(map[int64]*ci.Run),
		logEntries:    make(map[int64]*ci.LogEntry),
		nextID:        1,
	}
}

func (s *FakeCIStore) allocID() int64 {
	id := s.nextID
	s.nextID++
	return id
}

// --- RunnerRegistration operations ---

func (s *FakeCIStore) CreateRunnerRegistration(_ context.Context, name, dispatchURL, secretToken string) (*ci.RunnerRegistration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.CreateRunnerErr != nil {
		return nil, s.CreateRunnerErr
	}
	id := s.allocID()
	r := &ci.RunnerRegistration{ID: id, Name: name, DispatchURL: dispatchURL, SecretToken: secretToken}
	s.runners[id] = r
	s.runnersByName[name] = id
	res := *r
	return &res, nil
}

func (s *FakeCIStore) GetRunnerRegistration(_ context.Context, id int64) (*ci.RunnerRegistration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.GetRunnerErr != nil {
		return nil, s.GetRunnerErr
	}
	r, ok := s.runners[id]
	if !ok {
		return nil, ci.ErrRunnerRegistrationNotFound
	}
	res := *r
	return &res, nil
}

func (s *FakeCIStore) GetRunnerRegistrationByName(_ context.Context, name string) (*ci.RunnerRegistration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.GetRunnerErr != nil {
		return nil, s.GetRunnerErr
	}
	id, ok := s.runnersByName[name]
	if !ok {
		return nil, ci.ErrRunnerRegistrationNotFound
	}
	r, ok := s.runners[id]
	if !ok {
		return nil, ci.ErrRunnerRegistrationNotFound
	}
	res := *r
	return &res, nil
}

func (s *FakeCIStore) ListRunnerRegistrations(_ context.Context) ([]ci.RunnerRegistration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ListRunnersErr != nil {
		return nil, s.ListRunnersErr
	}
	var result []ci.RunnerRegistration
	for _, r := range s.runners {
		result = append(result, *r)
	}
	return result, nil
}

func (s *FakeCIStore) DeleteRunnerRegistration(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.DeleteRunnerErr != nil {
		return s.DeleteRunnerErr
	}
	r, ok := s.runners[id]
	if !ok {
		return ci.ErrRunnerRegistrationNotFound
	}
	delete(s.runnersByName, r.Name)
	delete(s.runners, id)
	return nil
}

// --- Workflow operations ---

func (s *FakeCIStore) UpsertWorkflow(_ context.Context, repoName, name, script, runsOn string, container *string, triggers []ci.EventType) (*ci.Workflow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.UpsertWorkflowErr != nil {
		return nil, s.UpsertWorkflowErr
	}
	// Look for existing workflow with same repo+name
	for _, w := range s.workflows {
		if w.RepoName == repoName && w.Name == name {
			w.Script = script
			w.RunsOn = runsOn
			w.Container = container
			w.Triggers = triggers
			res := *w
			return &res, nil
		}
	}
	// Create new
	id := s.allocID()
	w := &ci.Workflow{
		ID:        id,
		RepoName:  repoName,
		Name:      name,
		Script:    script,
		RunsOn:    runsOn,
		Container: container,
		Triggers:  triggers,
	}
	s.workflows[id] = w
	res := *w
	return &res, nil
}

func (s *FakeCIStore) ListWorkflowsByRepo(_ context.Context, repoName string) ([]ci.Workflow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ListWorkflowsErr != nil {
		return nil, s.ListWorkflowsErr
	}
	var result []ci.Workflow
	for _, w := range s.workflows {
		if w.RepoName == repoName {
			result = append(result, *w)
		}
	}
	return result, nil
}

func (s *FakeCIStore) DeleteStaleWorkflows(_ context.Context, repoName string, keepNames []string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.DeleteStaleWorkflowsErr != nil {
		return 0, s.DeleteStaleWorkflowsErr
	}
	keepSet := make(map[string]bool, len(keepNames))
	for _, n := range keepNames {
		keepSet[n] = true
	}
	var deleted int64
	for id, w := range s.workflows {
		if w.RepoName == repoName && !keepSet[w.Name] {
			delete(s.workflows, id)
			deleted++
		}
	}
	return deleted, nil
}

// --- Run operations ---

func (s *FakeCIStore) CreateRun(_ context.Context, repoName, workflowName, script, runsOn string, container *string, triggeredByEvent ci.EventType, createdAt time.Time) (*ci.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.CreateRunErr != nil {
		return nil, s.CreateRunErr
	}
	id := s.allocID()
	r := &ci.Run{
		ID:               id,
		RepoName:         repoName,
		WorkflowName:     workflowName,
		Script:           script,
		RunsOn:           runsOn,
		Container:        container,
		TriggeredByEvent: triggeredByEvent,
		Status:           ci.RunStatusPending,
		CreatedAt:        createdAt,
	}
	s.runs[id] = r
	res := *r
	return &res, nil
}

func (s *FakeCIStore) GetRun(_ context.Context, id int64) (*ci.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.GetRunErr != nil {
		return nil, s.GetRunErr
	}
	r, ok := s.runs[id]
	if !ok {
		return nil, ci.ErrRunNotFound
	}
	res := *r
	return &res, nil
}

func (s *FakeCIStore) ListRunsByRepo(_ context.Context, repoName string) ([]ci.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ListRunsErr != nil {
		return nil, s.ListRunsErr
	}
	var result []ci.Run
	for _, r := range s.runs {
		if r.RepoName == repoName {
			result = append(result, *r)
		}
	}
	return result, nil
}

func (s *FakeCIStore) ListRunsByStatus(_ context.Context, status ci.RunStatus) ([]ci.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ListRunsErr != nil {
		return nil, s.ListRunsErr
	}
	var result []ci.Run
	for _, r := range s.runs {
		if r.Status == status {
			result = append(result, *r)
		}
	}
	return result, nil
}

func (s *FakeCIStore) UpdateRunStatus(_ context.Context, id int64, status ci.RunStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.UpdateRunStatusErr != nil {
		return s.UpdateRunStatusErr
	}
	r, ok := s.runs[id]
	if !ok {
		return ci.ErrRunNotFound
	}
	r.Status = status
	return nil
}

func (s *FakeCIStore) UpdateRunStarted(_ context.Context, id int64, startedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.UpdateRunStartedErr != nil {
		return s.UpdateRunStartedErr
	}
	r, ok := s.runs[id]
	if !ok {
		return ci.ErrRunNotFound
	}
	r.Status = ci.RunStatusRunning
	r.StartedAt = &startedAt
	return nil
}

func (s *FakeCIStore) UpdateRunFinished(_ context.Context, id int64, status ci.RunStatus, finishedAt time.Time, failureReason *ci.FailureReason) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.UpdateRunFinishedErr != nil {
		return s.UpdateRunFinishedErr
	}
	r, ok := s.runs[id]
	if !ok {
		return ci.ErrRunNotFound
	}
	r.Status = status
	r.FinishedAt = &finishedAt
	r.FailureReason = failureReason
	return nil
}

func (s *FakeCIStore) DeleteRun(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.DeleteRunErr != nil {
		return s.DeleteRunErr
	}
	// Delete cascading LogEntries
	for lid, le := range s.logEntries {
		if le.RunID == id {
			delete(s.logEntries, lid)
		}
	}
	delete(s.runs, id)
	return nil
}

// --- LogEntry operations ---

func (s *FakeCIStore) CreateLogEntry(_ context.Context, runID int64, line string, receivedAt time.Time) (*ci.LogEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.CreateLogEntryErr != nil {
		return nil, s.CreateLogEntryErr
	}
	id := s.allocID()
	le := &ci.LogEntry{ID: id, RunID: runID, Line: line, ReceivedAt: receivedAt}
	s.logEntries[id] = le
	res := *le
	return &res, nil
}

func (s *FakeCIStore) ListLogEntriesByRun(_ context.Context, runID int64) ([]ci.LogEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ListLogEntriesErr != nil {
		return nil, s.ListLogEntriesErr
	}
	var result []ci.LogEntry
	for _, le := range s.logEntries {
		if le.RunID == runID {
			result = append(result, *le)
		}
	}
	return result, nil
}

// --- Fake RunnerDispatch ---

// FakeRunnerDispatch is an in-memory implementation of ci.RunnerDispatch.
// It simulates the runner's behavior without any real HTTP or process execution.
// Tests control the runner's responses through method callbacks.
type FakeRunnerDispatch struct {
	mu sync.RWMutex

	// Dispatch control
	DispatchErr       error                      // if set, Dispatch returns this error
	DispatchCallbacks map[int64]func()            // per-runID callbacks invoked on dispatch
	DispatchedRuns    map[int64]ci.RunnerRegistration // tracks dispatched runs

	// Cancel control
	CancelErr         error                      // if set, SendCancel returns this error
	CancelCallbacks   map[int64]func()            // per-runID callbacks invoked on cancel
	CanceledRuns      map[int64]struct{}          // tracks canceled runs

	// History
	DispatchHistory []DispatchRecord
	CancelHistory   []CancelRecord
}

// DispatchRecord records a dispatch attempt.
type DispatchRecord struct {
	RunID  int64
	Runner string
}

// CancelRecord records a cancel attempt.
type CancelRecord struct {
	RunID  int64
	Runner string
}

// NewFakeRunnerDispatch creates a new FakeRunnerDispatch.
func NewFakeRunnerDispatch() *FakeRunnerDispatch {
	return &FakeRunnerDispatch{
		DispatchCallbacks: make(map[int64]func()),
		DispatchedRuns:    make(map[int64]ci.RunnerRegistration),
		CancelCallbacks:   make(map[int64]func()),
		CanceledRuns:      make(map[int64]struct{}),
	}
}

// Dispatch sends a simulated dispatch webhook to the runner.
func (f *FakeRunnerDispatch) Dispatch(_ context.Context, runner *ci.RunnerRegistration, run *ci.Run) error {
	f.mu.Lock()
	f.DispatchHistory = append(f.DispatchHistory, DispatchRecord{RunID: run.ID, Runner: runner.Name})
	f.DispatchedRuns[run.ID] = *runner
	dispatchErr := f.DispatchErr
	cb := f.DispatchCallbacks[run.ID]
	f.mu.Unlock()

	if dispatchErr != nil {
		return fmt.Errorf("dispatch to %s failed: %w", runner.Name, dispatchErr)
	}

	// Invoke callback if set (allows test to react to dispatch)
	if cb != nil {
		cb()
	}

	return nil
}

// SendCancel sends a simulated cancel webhook to the runner.
func (f *FakeRunnerDispatch) SendCancel(_ context.Context, runner *ci.RunnerRegistration, run *ci.Run) error {
	f.mu.Lock()
	f.CancelHistory = append(f.CancelHistory, CancelRecord{RunID: run.ID, Runner: runner.Name})
	f.CanceledRuns[run.ID] = struct{}{}
	cancelErr := f.CancelErr
	cb := f.CancelCallbacks[run.ID]
	f.mu.Unlock()

	if cancelErr != nil {
		return fmt.Errorf("cancel to %s failed: %w", runner.Name, cancelErr)
	}

	// Invoke callback if set
	if cb != nil {
		cb()
	}

	return nil
}

// WasDispatched returns whether a run was dispatched.
func (f *FakeRunnerDispatch) WasDispatched(runID int64) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.DispatchedRuns[runID]
	return ok
}

// WasCanceled returns whether a cancel was sent to the runner.
func (f *FakeRunnerDispatch) WasCanceled(runID int64) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.CanceledRuns[runID]
	return ok
}

// DispatchCount returns the number of dispatch attempts.
func (f *FakeRunnerDispatch) DispatchCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.DispatchHistory)
}

// CancelCount returns the number of cancel attempts.
func (f *FakeRunnerDispatch) CancelCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.CancelHistory)
}

// --- Test fixture helper ---

// Setup configures a CIService with fake dependencies for testing.
type Setup struct {
	Service  *ci.CIService
	Store    *FakeCIStore
	Dispatch *FakeRunnerDispatch
	Clock    *FakeClock
}

// NewTestSetup creates a new test fixture with all fake dependencies.
func NewTestSetup() *Setup {
	store := NewFakeCIStore()
	dispatch := NewFakeRunnerDispatch()
	clock := NewFakeClock()
	cfg := ci.DefaultCIConfig()
	svc := ci.NewCIService(cfg, store, dispatch, clock)

	return &Setup{
		Service:  svc,
		Store:    store,
		Dispatch: dispatch,
		Clock:    clock,
	}
}
