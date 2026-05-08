// Package sqlstore is the SQL adapter for the ci.Store port.
//
// It depends on github.com/charmbracelet/soft-serve/pkg/db so it can
// share the configured driver, transaction wrapper and error
// translation layer with the rest of Soft Serve. Driver-specific
// errors (sql.ErrNoRows, pq/sqlite duplicate key, ...) are translated
// at this seam: the ci package only sees the sentinels declared in
// pkg/ci/errors.go.
package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/ci"
	"github.com/charmbracelet/soft-serve/pkg/db"
)

// Store implements ci.Store on top of a *db.DB.
type Store struct {
	db *db.DB
}

var _ ci.Store = (*Store)(nil)

// New constructs a Store. The migrations registering ci_* tables must
// have run on the database before the Store is used.
func New(database *db.DB) *Store {
	return &Store{db: database}
}

// --- Runner registrations -------------------------------------------------

func (s *Store) SaveRunnerRegistration(ctx context.Context, registration ci.RunnerRegistration) error {
	return s.db.TransactionContext(ctx, func(tx *db.Tx) error {
		// Upsert: try update first, insert if no row exists.
		query := tx.Rebind(`UPDATE ci_runner_registrations
			SET dispatch_url = ?, secret_token = ?, updated_at = CURRENT_TIMESTAMP
			WHERE name = ?;`)
		result, err := tx.ExecContext(ctx, query, registration.DispatchURL, registration.SecretToken, registration.Name)
		if err != nil {
			return db.WrapError(err)
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			query = tx.Rebind(`INSERT INTO ci_runner_registrations (name, dispatch_url, secret_token)
				VALUES (?, ?, ?);`)
			if _, err := tx.ExecContext(ctx, query, registration.Name, registration.DispatchURL, registration.SecretToken); err != nil {
				return db.WrapError(err)
			}
		}
		return nil
	})
}

func (s *Store) GetRunnerRegistration(ctx context.Context, name string) (*ci.RunnerRegistration, error) {
	var row runnerRegistrationRow
	query := s.db.Rebind(`SELECT name, dispatch_url, secret_token
		FROM ci_runner_registrations WHERE name = ?;`)
	if err := s.db.GetContext(ctx, &row, query, name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ci.ErrRunnerRegistrationNotFound
		}
		return nil, db.WrapError(err)
	}
	registration := row.toRegistration()
	return &registration, nil
}

func (s *Store) RemoveRunnerRegistration(ctx context.Context, name string) error {
	query := s.db.Rebind(`DELETE FROM ci_runner_registrations WHERE name = ?;`)
	_, err := s.db.ExecContext(ctx, query, name)
	return db.WrapError(err)
}

// ListRunnerRegistrations returns every registered runner. This is
// not part of the ci.Store port (the domain Service has no rule that
// scans all registrations); it exists for the CLI and any future
// admin surfaces.
func (s *Store) ListRunnerRegistrations(ctx context.Context) ([]ci.RunnerRegistration, error) {
	var rows []runnerRegistrationRow
	query := s.db.Rebind(`SELECT name, dispatch_url, secret_token
		FROM ci_runner_registrations ORDER BY name ASC;`)
	if err := s.db.SelectContext(ctx, &rows, query); err != nil {
		return nil, db.WrapError(err)
	}
	out := make([]ci.RunnerRegistration, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toRegistration())
	}
	return out, nil
}

// --- Workflows ------------------------------------------------------------

func (s *Store) UpsertWorkflow(ctx context.Context, workflow ci.Workflow) error {
	triggers, err := encodeTriggers(workflow.Triggers)
	if err != nil {
		return fmt.Errorf("encode triggers: %w", err)
	}
	return s.db.TransactionContext(ctx, func(tx *db.Tx) error {
		query := tx.Rebind(`UPDATE ci_workflows
			SET script = ?, runs_on = ?, container = ?, triggers = ?, updated_at = CURRENT_TIMESTAMP
			WHERE repo_name = ? AND name = ?;`)
		result, err := tx.ExecContext(ctx, query,
			workflow.Script, workflow.RunsOn, ptrString(workflow.Container), triggers,
			workflow.RepoName, workflow.Name)
		if err != nil {
			return db.WrapError(err)
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			query = tx.Rebind(`INSERT INTO ci_workflows (repo_name, name, script, runs_on, container, triggers)
				VALUES (?, ?, ?, ?, ?, ?);`)
			if _, err := tx.ExecContext(ctx, query,
				workflow.RepoName, workflow.Name, workflow.Script, workflow.RunsOn,
				ptrString(workflow.Container), triggers); err != nil {
				return db.WrapError(err)
			}
		}
		return nil
	})
}

func (s *Store) DeleteWorkflowsExcept(ctx context.Context, repoName string, keep map[string]bool) error {
	return s.db.TransactionContext(ctx, func(tx *db.Tx) error {
		var rows []workflowRow
		query := tx.Rebind(`SELECT id, repo_name, name, script, runs_on, container, triggers
			FROM ci_workflows WHERE repo_name = ?;`)
		if err := tx.SelectContext(ctx, &rows, query, repoName); err != nil {
			return db.WrapError(err)
		}
		for _, row := range rows {
			if keep[row.Name] {
				continue
			}
			del := tx.Rebind(`DELETE FROM ci_workflows WHERE id = ?;`)
			if _, err := tx.ExecContext(ctx, del, row.ID); err != nil {
				return db.WrapError(err)
			}
		}
		return nil
	})
}

func (s *Store) ListWorkflowsByRepo(ctx context.Context, repoName string) ([]ci.Workflow, error) {
	var rows []workflowRow
	query := s.db.Rebind(`SELECT id, repo_name, name, script, runs_on, container, triggers
		FROM ci_workflows WHERE repo_name = ? ORDER BY name ASC;`)
	if err := s.db.SelectContext(ctx, &rows, query, repoName); err != nil {
		return nil, db.WrapError(err)
	}
	out := make([]ci.Workflow, 0, len(rows))
	for _, row := range rows {
		workflow, err := row.toWorkflow()
		if err != nil {
			return nil, fmt.Errorf("decode workflow %q: %w", row.Name, err)
		}
		out = append(out, workflow)
	}
	return out, nil
}

// --- Runs -----------------------------------------------------------------

func (s *Store) CreateRun(ctx context.Context, run ci.Run) (*ci.Run, error) {
	var inserted runRow
	query := s.db.Rebind(`INSERT INTO ci_runs
		(repo_name, workflow_name, script, runs_on, container, triggered_by_event,
		 status, created_at, started_at, finished_at, failure_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, repo_name, workflow_name, script, runs_on, container,
			triggered_by_event, status, created_at, started_at, finished_at, failure_reason;`)
	args := []interface{}{
		run.RepoName, run.WorkflowName, run.Script, run.RunsOn,
		ptrString(run.Container), string(run.TriggeredByEvent), string(run.Status),
		run.CreatedAt, ptrTime(run.StartedAt), ptrTime(run.FinishedAt),
		ptrFailureReason(run.FailureReason),
	}
	if err := s.db.GetContext(ctx, &inserted, query, args...); err != nil {
		// Drivers without RETURNING: insert + fetch by LastInsertId.
		insert := s.db.Rebind(`INSERT INTO ci_runs
			(repo_name, workflow_name, script, runs_on, container, triggered_by_event,
			 status, created_at, started_at, finished_at, failure_reason)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`)
		result, ierr := s.db.ExecContext(ctx, insert, args...)
		if ierr != nil {
			return nil, db.WrapError(ierr)
		}
		id, _ := result.LastInsertId()
		sel := s.db.Rebind(`SELECT id, repo_name, workflow_name, script, runs_on, container,
			triggered_by_event, status, created_at, started_at, finished_at, failure_reason
			FROM ci_runs WHERE id = ?;`)
		if err := s.db.GetContext(ctx, &inserted, sel, id); err != nil {
			return nil, db.WrapError(err)
		}
	}
	out := inserted.toRun()
	return &out, nil
}

func (s *Store) GetRun(ctx context.Context, id int64) (*ci.Run, error) {
	var row runRow
	query := s.db.Rebind(`SELECT id, repo_name, workflow_name, script, runs_on, container,
		triggered_by_event, status, created_at, started_at, finished_at, failure_reason
		FROM ci_runs WHERE id = ?;`)
	if err := s.db.GetContext(ctx, &row, query, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ci.ErrRunNotFound
		}
		return nil, db.WrapError(err)
	}
	out := row.toRun()
	return &out, nil
}

func (s *Store) UpdateRun(ctx context.Context, run ci.Run) error {
	query := s.db.Rebind(`UPDATE ci_runs
		SET repo_name = ?, workflow_name = ?, script = ?, runs_on = ?, container = ?,
			triggered_by_event = ?, status = ?, created_at = ?, started_at = ?,
			finished_at = ?, failure_reason = ?
		WHERE id = ?;`)
	result, err := s.db.ExecContext(ctx, query,
		run.RepoName, run.WorkflowName, run.Script, run.RunsOn,
		ptrString(run.Container), string(run.TriggeredByEvent), string(run.Status),
		run.CreatedAt, ptrTime(run.StartedAt), ptrTime(run.FinishedAt),
		ptrFailureReason(run.FailureReason), run.ID)
	if err != nil {
		return db.WrapError(err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ci.ErrRunNotFound
	}
	return nil
}

func (s *Store) ListRuns(ctx context.Context) ([]ci.Run, error) {
	var rows []runRow
	query := s.db.Rebind(`SELECT id, repo_name, workflow_name, script, runs_on, container,
		triggered_by_event, status, created_at, started_at, finished_at, failure_reason
		FROM ci_runs ORDER BY id ASC;`)
	if err := s.db.SelectContext(ctx, &rows, query); err != nil {
		return nil, db.WrapError(err)
	}
	out := make([]ci.Run, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toRun())
	}
	return out, nil
}

func (s *Store) CreateLogEntry(ctx context.Context, entry ci.LogEntry) error {
	query := s.db.Rebind(`INSERT INTO ci_log_entries (run_id, line, received_at)
		VALUES (?, ?, ?);`)
	_, err := s.db.ExecContext(ctx, query, entry.RunID, entry.Line, entry.ReceivedAt)
	return db.WrapError(err)
}

func (s *Store) ListLogEntriesByRun(ctx context.Context, runID int64) ([]ci.LogEntry, error) {
	var rows []logEntryRow
	query := s.db.Rebind(`SELECT id, run_id, line, received_at
		FROM ci_log_entries WHERE run_id = ? ORDER BY id ASC;`)
	if err := s.db.SelectContext(ctx, &rows, query, runID); err != nil {
		return nil, db.WrapError(err)
	}
	out := make([]ci.LogEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toLogEntry())
	}
	return out, nil
}

func (s *Store) DeleteRun(ctx context.Context, runID int64) error {
	return s.db.TransactionContext(ctx, func(tx *db.Tx) error {
		// SQLite enforces foreign keys with ON DELETE CASCADE only when
		// PRAGMA foreign_keys=ON; not all configurations enable it.
		// Delete log entries explicitly to keep behaviour identical
		// across drivers and PRAGMA settings.
		delLogs := tx.Rebind(`DELETE FROM ci_log_entries WHERE run_id = ?;`)
		if _, err := tx.ExecContext(ctx, delLogs, runID); err != nil {
			return db.WrapError(err)
		}
		delRun := tx.Rebind(`DELETE FROM ci_runs WHERE id = ?;`)
		if _, err := tx.ExecContext(ctx, delRun, runID); err != nil {
			return db.WrapError(err)
		}
		return nil
	})
}

// --- Row types and helpers ------------------------------------------------

type runnerRegistrationRow struct {
	Name        string `db:"name"`
	DispatchURL string `db:"dispatch_url"`
	SecretToken string `db:"secret_token"`
}

func (r runnerRegistrationRow) toRegistration() ci.RunnerRegistration {
	return ci.RunnerRegistration{
		Name:        r.Name,
		DispatchURL: r.DispatchURL,
		SecretToken: r.SecretToken,
	}
}

type workflowRow struct {
	ID       int64          `db:"id"`
	RepoName string         `db:"repo_name"`
	Name     string         `db:"name"`
	Script   string         `db:"script"`
	RunsOn   string         `db:"runs_on"`
	Container sql.NullString `db:"container"`
	Triggers string         `db:"triggers"`
}

func (r workflowRow) toWorkflow() (ci.Workflow, error) {
	triggers, err := decodeTriggers(r.Triggers)
	if err != nil {
		return ci.Workflow{}, err
	}
	w := ci.Workflow{
		RepoName: r.RepoName,
		Name:     r.Name,
		Script:   r.Script,
		RunsOn:   r.RunsOn,
		Triggers: triggers,
	}
	if r.Container.Valid {
		c := r.Container.String
		w.Container = &c
	}
	return w, nil
}

type runRow struct {
	ID               int64          `db:"id"`
	RepoName         string         `db:"repo_name"`
	WorkflowName     string         `db:"workflow_name"`
	Script           string         `db:"script"`
	RunsOn           string         `db:"runs_on"`
	Container        sql.NullString `db:"container"`
	TriggeredByEvent string         `db:"triggered_by_event"`
	Status           string         `db:"status"`
	CreatedAt        time.Time      `db:"created_at"`
	StartedAt        sql.NullTime   `db:"started_at"`
	FinishedAt       sql.NullTime   `db:"finished_at"`
	FailureReason    sql.NullString `db:"failure_reason"`
}

func (r runRow) toRun() ci.Run {
	run := ci.Run{
		ID:               r.ID,
		RepoName:         r.RepoName,
		WorkflowName:     r.WorkflowName,
		Script:           r.Script,
		RunsOn:           r.RunsOn,
		TriggeredByEvent: ci.EventType(r.TriggeredByEvent),
		Status:           ci.RunStatus(r.Status),
		CreatedAt:        r.CreatedAt,
	}
	if r.Container.Valid {
		c := r.Container.String
		run.Container = &c
	}
	if r.StartedAt.Valid {
		t := r.StartedAt.Time
		run.StartedAt = &t
	}
	if r.FinishedAt.Valid {
		t := r.FinishedAt.Time
		run.FinishedAt = &t
	}
	if r.FailureReason.Valid {
		fr := ci.FailureReason(r.FailureReason.String)
		run.FailureReason = &fr
	}
	return run
}

type logEntryRow struct {
	ID         int64     `db:"id"`
	RunID      int64     `db:"run_id"`
	Line       string    `db:"line"`
	ReceivedAt time.Time `db:"received_at"`
}

func (r logEntryRow) toLogEntry() ci.LogEntry {
	return ci.LogEntry{
		ID:         r.ID,
		RunID:      r.RunID,
		Line:       r.Line,
		ReceivedAt: r.ReceivedAt,
	}
}

// encodeTriggers serialises a set of EventType values as a sorted JSON
// array. Sorting keeps the encoding deterministic so two writes with
// the same logical set produce the same bytes — useful for diffing
// during workflow sync.
func encodeTriggers(triggers map[ci.EventType]bool) (string, error) {
	values := make([]string, 0, len(triggers))
	for eventType, enabled := range triggers {
		if enabled {
			values = append(values, string(eventType))
		}
	}
	sort.Strings(values)
	bytes, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func decodeTriggers(encoded string) (map[ci.EventType]bool, error) {
	if encoded == "" {
		return map[ci.EventType]bool{}, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		return nil, err
	}
	out := make(map[ci.EventType]bool, len(values))
	for _, value := range values {
		out[ci.EventType(value)] = true
	}
	return out, nil
}

func ptrString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func ptrTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func ptrFailureReason(fr *ci.FailureReason) sql.NullString {
	if fr == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(*fr), Valid: true}
}
