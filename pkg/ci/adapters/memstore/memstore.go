// Package memstore is an in-memory ci.Store adapter.
//
// It is the reference implementation that both the unit tests for
// ci.Service and the shared store contract test exercise. Adapters
// that diverge from it have a bug; this is what keeps the fake
// honest.
//
// memstore is safe for concurrent use within a single process.
package memstore

import (
	"context"
	"sort"
	"sync"

	"github.com/charmbracelet/soft-serve/pkg/ci"
)

// Store is an in-memory ci.Store.
type Store struct {
	mu sync.Mutex

	runners   map[string]ci.RunnerRegistration
	workflows map[string]map[string]ci.Workflow
	runs      map[int64]ci.Run
	logs      map[int64][]ci.LogEntry

	nextRunID int64
	nextLogID int64
}

var _ ci.Store = (*Store)(nil)

// New constructs an empty Store.
func New() *Store {
	return &Store{
		runners:   make(map[string]ci.RunnerRegistration),
		workflows: make(map[string]map[string]ci.Workflow),
		runs:      make(map[int64]ci.Run),
		logs:      make(map[int64][]ci.LogEntry),
	}
}

func (s *Store) SaveRunnerRegistration(_ context.Context, registration ci.RunnerRegistration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runners[registration.Name] = registration
	return nil
}

func (s *Store) GetRunnerRegistration(_ context.Context, name string) (*ci.RunnerRegistration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	registration, ok := s.runners[name]
	if !ok {
		return nil, ci.ErrRunnerRegistrationNotFound
	}
	return &registration, nil
}

func (s *Store) RemoveRunnerRegistration(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runners, name)
	return nil
}

func (s *Store) UpsertWorkflow(_ context.Context, workflow ci.Workflow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workflows[workflow.RepoName] == nil {
		s.workflows[workflow.RepoName] = make(map[string]ci.Workflow)
	}
	s.workflows[workflow.RepoName][workflow.Name] = cloneWorkflow(workflow)
	return nil
}

func (s *Store) DeleteWorkflowsExcept(_ context.Context, repoName string, keep map[string]bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name := range s.workflows[repoName] {
		if !keep[name] {
			delete(s.workflows[repoName], name)
		}
	}
	return nil
}

func (s *Store) ListWorkflowsByRepo(_ context.Context, repoName string) ([]ci.Workflow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workflows := make([]ci.Workflow, 0, len(s.workflows[repoName]))
	for _, workflow := range s.workflows[repoName] {
		workflows = append(workflows, cloneWorkflow(workflow))
	}
	sort.Slice(workflows, func(i, j int) bool { return workflows[i].Name < workflows[j].Name })
	return workflows, nil
}

func (s *Store) CreateRun(_ context.Context, run ci.Run) (*ci.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextRunID++
	run.ID = s.nextRunID
	s.runs[run.ID] = cloneRun(run)
	cloned := cloneRun(run)
	return &cloned, nil
}

func (s *Store) GetRun(_ context.Context, id int64) (*ci.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok {
		return nil, ci.ErrRunNotFound
	}
	cloned := cloneRun(run)
	return &cloned, nil
}

func (s *Store) UpdateRun(_ context.Context, run ci.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[run.ID]; !ok {
		return ci.ErrRunNotFound
	}
	s.runs[run.ID] = cloneRun(run)
	return nil
}

func (s *Store) ListRuns(_ context.Context) ([]ci.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	runs := make([]ci.Run, 0, len(s.runs))
	for _, run := range s.runs {
		runs = append(runs, cloneRun(run))
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })
	return runs, nil
}

func (s *Store) CreateLogEntry(_ context.Context, entry ci.LogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextLogID++
	entry.ID = s.nextLogID
	s.logs[entry.RunID] = append(s.logs[entry.RunID], entry)
	return nil
}

func (s *Store) ListLogEntriesByRun(_ context.Context, runID int64) ([]ci.LogEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	logs := make([]ci.LogEntry, len(s.logs[runID]))
	copy(logs, s.logs[runID])
	return logs, nil
}

func (s *Store) DeleteRun(_ context.Context, runID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runs, runID)
	delete(s.logs, runID)
	return nil
}

func cloneWorkflow(workflow ci.Workflow) ci.Workflow {
	if workflow.Container != nil {
		container := *workflow.Container
		workflow.Container = &container
	}
	if workflow.Triggers != nil {
		triggers := make(map[ci.EventType]bool, len(workflow.Triggers))
		for eventType, enabled := range workflow.Triggers {
			triggers[eventType] = enabled
		}
		workflow.Triggers = triggers
	}
	return workflow
}

func cloneRun(run ci.Run) ci.Run {
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
