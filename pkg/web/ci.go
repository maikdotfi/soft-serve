// Package web — CI HTTP surface.
//
// CIController mounts the two HTTP surfaces declared in ci.allium:
//
//   - RunnerDispatchAdapter (POST endpoints under /api/v1/ci/runs/{id}/...):
//     the runner reports lifecycle and logs back to Soft Serve. Auth is
//     a Bearer token equal to the runner's secret_token (issued at
//     registration). Token mismatch ⇒ 403; unknown run ⇒ 404; bad JSON
//     ⇒ 400; transition violation ⇒ 409.
//
//   - RunQueryAPI (GET endpoints under /api/v1/ci/runs and
//     /api/v1/ci/workflows): authenticated read of runs, log entries
//     and workflows. v1 does not yet enforce per-repo ACLs on these
//     endpoints (the spec defers fine-grained authorization); auth is
//     required (401 on no/bad credentials) but any authenticated user
//     sees every run.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/ci"
	"github.com/gorilla/mux"
)

// CIController registers the CI HTTP routes on r. If the CI service
// is not (or has not yet been) wired on the backend, the routes are
// still registered but each will respond with 503 Service
// Unavailable, matching the existing /readyz pattern.
func CIController(_ context.Context, r *mux.Router) {
	api := r.PathPrefix("/api/v1/ci").Subrouter()

	// Runner-callback (RunnerDispatchAdapter)
	api.HandleFunc("/runs/{id:[0-9]+}/started", ciRunnerStarted).Methods(http.MethodPost)
	api.HandleFunc("/runs/{id:[0-9]+}/completion", ciRunnerCompletion).Methods(http.MethodPost)
	api.HandleFunc("/runs/{id:[0-9]+}/logs", ciRunnerLogLine).Methods(http.MethodPost)

	// RunQueryAPI (read-only)
	api.HandleFunc("/runs", ciListRuns).Methods(http.MethodGet)
	api.HandleFunc("/runs/{id:[0-9]+}", ciGetRun).Methods(http.MethodGet)
	api.HandleFunc("/runs/{id:[0-9]+}/logs", ciListRunLogs).Methods(http.MethodGet)
	api.HandleFunc("/workflows", ciListWorkflows).Methods(http.MethodGet)
}

// --- Runner-callback handlers --------------------------------------

func ciRunnerStarted(w http.ResponseWriter, r *http.Request) {
	svc, ok := ciServiceOrUnavailable(w, r)
	if !ok {
		return
	}
	id, ok := ciRunIDFromPath(w, r)
	if !ok {
		return
	}
	token, ok := ciBearerOrUnauthorized(w, r)
	if !ok {
		return
	}
	if err := svc.ReportStarted(r.Context(), token, id); err != nil {
		ciTranslateError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func ciRunnerCompletion(w http.ResponseWriter, r *http.Request) {
	svc, ok := ciServiceOrUnavailable(w, r)
	if !ok {
		return
	}
	id, ok := ciRunIDFromPath(w, r)
	if !ok {
		return
	}
	token, ok := ciBearerOrUnauthorized(w, r)
	if !ok {
		return
	}

	var body struct {
		ExitCode int `json:"exit_code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if err := svc.ReportCompletion(r.Context(), token, id, body.ExitCode); err != nil {
		ciTranslateError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func ciRunnerLogLine(w http.ResponseWriter, r *http.Request) {
	svc, ok := ciServiceOrUnavailable(w, r)
	if !ok {
		return
	}
	id, ok := ciRunIDFromPath(w, r)
	if !ok {
		return
	}
	token, ok := ciBearerOrUnauthorized(w, r)
	if !ok {
		return
	}

	var body struct {
		Line string `json:"line"`
	}
	// 64 KiB cap matches the conservative upper bound for a single
	// log line; runners that emit longer lines must split them.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if err := svc.IngestLogLine(r.Context(), token, id, body.Line); err != nil {
		ciTranslateError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// --- Run query handlers --------------------------------------------
//
// v1 returns the raw domain types as JSON. Authentication is
// required but per-repo ACLs are deferred per ci.allium ("fine ACLs
// hardening is explicitly deferred").

type ciRunDTO struct {
	ID               int64   `json:"id"`
	RepoName         string  `json:"repo_name"`
	WorkflowName     string  `json:"workflow_name"`
	Script           string  `json:"script"`
	RunsOn           string  `json:"runs_on"`
	Container        *string `json:"container,omitempty"`
	TriggeredByEvent string  `json:"triggered_by_event"`
	Status           string  `json:"status"`
	CreatedAt        string  `json:"created_at"`
	StartedAt        *string `json:"started_at,omitempty"`
	FinishedAt       *string `json:"finished_at,omitempty"`
	FailureReason    *string `json:"failure_reason,omitempty"`
}

func toCIRunDTO(run ci.Run) ciRunDTO {
	dto := ciRunDTO{
		ID:               run.ID,
		RepoName:         run.RepoName,
		WorkflowName:     run.WorkflowName,
		Script:           run.Script,
		RunsOn:           run.RunsOn,
		Container:        run.Container,
		TriggeredByEvent: string(run.TriggeredByEvent),
		Status:           string(run.Status),
		CreatedAt:        run.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if run.StartedAt != nil {
		s := run.StartedAt.UTC().Format("2006-01-02T15:04:05Z")
		dto.StartedAt = &s
	}
	if run.FinishedAt != nil {
		f := run.FinishedAt.UTC().Format("2006-01-02T15:04:05Z")
		dto.FinishedAt = &f
	}
	if run.FailureReason != nil {
		s := string(*run.FailureReason)
		dto.FailureReason = &s
	}
	return dto
}

type ciLogEntryDTO struct {
	ID         int64  `json:"id"`
	RunID      int64  `json:"run_id"`
	Line       string `json:"line"`
	ReceivedAt string `json:"received_at"`
}

type ciWorkflowDTO struct {
	RepoName  string   `json:"repo_name"`
	Name      string   `json:"name"`
	Script    string   `json:"script"`
	RunsOn    string   `json:"runs_on"`
	Container *string  `json:"container,omitempty"`
	Triggers  []string `json:"triggers"`
}

func ciListRuns(w http.ResponseWriter, r *http.Request) {
	svc, ok := ciServiceOrUnavailable(w, r)
	if !ok {
		return
	}
	runs, err := svc.ListAllRuns(r.Context())
	if err != nil {
		ciInternal(w, r, err)
		return
	}
	dtos := make([]ciRunDTO, 0, len(runs))
	for _, run := range runs {
		dtos = append(dtos, toCIRunDTO(run))
	}
	ciWriteJSON(w, http.StatusOK, dtos)
}

func ciGetRun(w http.ResponseWriter, r *http.Request) {
	svc, ok := ciServiceOrUnavailable(w, r)
	if !ok {
		return
	}
	id, ok := ciRunIDFromPath(w, r)
	if !ok {
		return
	}
	run, err := svc.GetRun(r.Context(), id)
	if err != nil {
		if errors.Is(err, ci.ErrRunNotFound) {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}
		ciInternal(w, r, err)
		return
	}
	ciWriteJSON(w, http.StatusOK, toCIRunDTO(run))
}

func ciListRunLogs(w http.ResponseWriter, r *http.Request) {
	svc, ok := ciServiceOrUnavailable(w, r)
	if !ok {
		return
	}
	id, ok := ciRunIDFromPath(w, r)
	if !ok {
		return
	}
	logs, err := svc.ListLogEntries(r.Context(), id)
	if err != nil {
		ciInternal(w, r, err)
		return
	}
	dtos := make([]ciLogEntryDTO, 0, len(logs))
	for _, entry := range logs {
		dtos = append(dtos, ciLogEntryDTO{
			ID:         entry.ID,
			RunID:      entry.RunID,
			Line:       entry.Line,
			ReceivedAt: entry.ReceivedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	ciWriteJSON(w, http.StatusOK, dtos)
}

func ciListWorkflows(w http.ResponseWriter, r *http.Request) {
	svc, ok := ciServiceOrUnavailable(w, r)
	if !ok {
		return
	}
	repoName := r.URL.Query().Get("repo")
	if repoName == "" {
		http.Error(w, "missing required query parameter 'repo'", http.StatusBadRequest)
		return
	}
	workflows, err := svc.ListWorkflowsByRepo(r.Context(), repoName)
	if err != nil {
		ciInternal(w, r, err)
		return
	}
	dtos := make([]ciWorkflowDTO, 0, len(workflows))
	for _, w := range workflows {
		triggers := make([]string, 0, len(w.Triggers))
		for et, on := range w.Triggers {
			if on {
				triggers = append(triggers, string(et))
			}
		}
		dtos = append(dtos, ciWorkflowDTO{
			RepoName:  w.RepoName,
			Name:      w.Name,
			Script:    w.Script,
			RunsOn:    w.RunsOn,
			Container: w.Container,
			Triggers:  triggers,
		})
	}
	ciWriteJSON(w, http.StatusOK, dtos)
}

// --- Helpers --------------------------------------------------------

func ciServiceOrUnavailable(w http.ResponseWriter, r *http.Request) (*ci.Service, bool) {
	be := backend.FromContext(r.Context())
	if be == nil || be.CIService() == nil {
		http.Error(w, "ci subsystem not configured", http.StatusServiceUnavailable)
		return nil, false
	}
	return be.CIService(), true
}

func ciRunIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "invalid run id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func ciBearerOrUnauthorized(w http.ResponseWriter, r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return "", false
	}
	return token, true
}

func ciTranslateError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ci.ErrUnauthorizedRunner):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, ci.ErrRunNotFound):
		http.Error(w, "run not found", http.StatusNotFound)
	case errors.Is(err, ci.ErrInvalidTransition):
		http.Error(w, "invalid run state for this operation", http.StatusConflict)
	default:
		ciInternal(w, r, err)
	}
}

func ciInternal(w http.ResponseWriter, r *http.Request, err error) {
	log.FromContext(r.Context()).Error("ci http handler error",
		"path", r.URL.Path, "method", r.Method, "err", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

func ciWriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Body has already been started; nothing useful to do but log.
		_, _ = io.WriteString(w, "")
	}
}
