package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/ci"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/memstore"
	"github.com/charmbracelet/soft-serve/pkg/config"
	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/db/migrate"
	"github.com/charmbracelet/soft-serve/pkg/store/database"
	"github.com/gorilla/mux"
	"github.com/matryer/is"
)

// ciTestEnv builds a router with the CI controller mounted on top of
// an in-memory ci.Store, plus a *Backend and *DB so the route handlers
// can pull things out of context exactly as they do in production.
type ciTestEnv struct {
	router *mux.Router
	svc    *ci.Service
	store  *memstore.Store
	be     *backend.Backend
	ctx    context.Context
}

func newCITestEnv(t *testing.T) *ciTestEnv {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()

	cfg := &config.Config{
		DataPath: tmp,
		DB: config.DBConfig{
			Driver:     "sqlite",
			DataSource: filepath.Join(tmp, "test.db") + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)",
		},
	}

	dbx, err := db.Open(ctx, cfg.DB.Driver, cfg.DB.DataSource)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { dbx.Close() })

	ctx = config.WithContext(ctx, cfg)
	ctx = log.WithContext(ctx, log.New(io.Discard))
	ctx = db.WithContext(ctx, dbx)
	if err := migrate.Migrate(ctx, dbx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	dbstore := database.New(ctx, dbx)
	be := backend.New(ctx, cfg, dbx, dbstore)
	ctx = backend.WithContext(ctx, be)

	store := memstore.New()
	ws := newStubSource()
	disp := stubCIDispatcher{}
	tokens := stubCITokens{}
	clock := stubCIClock{}
	svc := ci.NewService(ci.DefaultConfig(), store, ws, disp, tokens, clock, nil)
	be.SetCIService(svc)

	router := mux.NewRouter()
	CIController(ctx, router)

	// The CI controller's handlers read the service from the
	// request context, so the test wraps the router with a
	// middleware that injects the same context the production
	// NewContextHandler installs.
	wrapped := mux.NewRouter()
	wrapped.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(ctx)
		router.ServeHTTP(w, r)
	}))

	return &ciTestEnv{router: wrapped, svc: svc, store: store, be: be, ctx: ctx}
}

// TestCIRunnerStartedRoute verifies POST /api/v1/ci/runs/{id}/started
// authorises with the runner's bearer token and transitions the run
// from dispatched to running.
func TestCIRunnerStartedRoute(t *testing.T) {
	is := is.New(t)
	env := newCITestEnv(t)

	// Seed: registered runner + a dispatched run for it.
	is.NoErr(env.store.SaveRunnerRegistration(env.ctx, ci.RunnerRegistration{
		Name: "linux-amd64", DispatchURL: "http://runner.example", SecretToken: "secret",
	}))
	created, err := env.store.CreateRun(env.ctx, ci.Run{
		RepoName: "repo1", WorkflowName: "unit", RunsOn: "linux-amd64",
		Script: "go test", Status: ci.RunDispatched, TriggeredByEvent: ci.EventTypePush,
	})
	is.NoErr(err)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/ci/runs/"+strconv.FormatInt(created.ID, 10)+"/started", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	env.router.ServeHTTP(rr, req)

	is.Equal(rr.Code, http.StatusOK)
	got, err := env.store.GetRun(env.ctx, created.ID)
	is.NoErr(err)
	is.Equal(got.Status, ci.RunRunning)
}

// TestCIRunnerStartedRoute_RejectsWrongToken verifies that a bearer
// token mismatched to the run's runner returns 403.
func TestCIRunnerStartedRoute_RejectsWrongToken(t *testing.T) {
	is := is.New(t)
	env := newCITestEnv(t)

	is.NoErr(env.store.SaveRunnerRegistration(env.ctx, ci.RunnerRegistration{
		Name: "linux-amd64", DispatchURL: "http://runner.example", SecretToken: "secret",
	}))
	created, err := env.store.CreateRun(env.ctx, ci.Run{
		RepoName: "repo1", WorkflowName: "unit", RunsOn: "linux-amd64",
		Script: "go test", Status: ci.RunDispatched, TriggeredByEvent: ci.EventTypePush,
	})
	is.NoErr(err)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/ci/runs/"+strconv.FormatInt(created.ID, 10)+"/started", nil)
	req.Header.Set("Authorization", "Bearer not-the-real-token")
	rr := httptest.NewRecorder()
	env.router.ServeHTTP(rr, req)

	is.Equal(rr.Code, http.StatusForbidden)
}

// TestCIRunnerCompletionRoute_TerminalSuccess verifies the completion
// endpoint accepts an exit code in the body and transitions to
// succeeded on zero.
func TestCIRunnerCompletionRoute_TerminalSuccess(t *testing.T) {
	is := is.New(t)
	env := newCITestEnv(t)

	is.NoErr(env.store.SaveRunnerRegistration(env.ctx, ci.RunnerRegistration{
		Name: "linux-amd64", SecretToken: "secret",
	}))
	created, err := env.store.CreateRun(env.ctx, ci.Run{
		RepoName: "repo1", WorkflowName: "unit", RunsOn: "linux-amd64",
		Script: "go test", Status: ci.RunRunning, TriggeredByEvent: ci.EventTypePush,
	})
	is.NoErr(err)

	body := bytes.NewBufferString(`{"exit_code":0}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/ci/runs/"+strconv.FormatInt(created.ID, 10)+"/completion", body)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	env.router.ServeHTTP(rr, req)

	is.Equal(rr.Code, http.StatusOK)
	got, err := env.store.GetRun(env.ctx, created.ID)
	is.NoErr(err)
	is.Equal(got.Status, ci.RunSucceeded)
}

// TestCIRunnerLogsRoute appends a log line to a running run.
func TestCIRunnerLogsRoute(t *testing.T) {
	is := is.New(t)
	env := newCITestEnv(t)

	is.NoErr(env.store.SaveRunnerRegistration(env.ctx, ci.RunnerRegistration{
		Name: "linux-amd64", SecretToken: "secret",
	}))
	created, err := env.store.CreateRun(env.ctx, ci.Run{
		RepoName: "repo1", WorkflowName: "unit", RunsOn: "linux-amd64",
		Script: "go test", Status: ci.RunRunning, TriggeredByEvent: ci.EventTypePush,
	})
	is.NoErr(err)

	body := bytes.NewBufferString(`{"line":"PASS"}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/ci/runs/"+strconv.FormatInt(created.ID, 10)+"/logs", body)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	env.router.ServeHTTP(rr, req)

	is.Equal(rr.Code, http.StatusOK)

	logs, err := env.store.ListLogEntriesByRun(env.ctx, created.ID)
	is.NoErr(err)
	is.Equal(len(logs), 1)
	is.Equal(logs[0].Line, "PASS")
}

// TestCIQueryRunsRoute returns the list of runs as JSON.
func TestCIQueryRunsRoute(t *testing.T) {
	is := is.New(t)
	env := newCITestEnv(t)

	_, err := env.store.CreateRun(env.ctx, ci.Run{
		RepoName: "repo1", WorkflowName: "unit", RunsOn: "linux-amd64",
		Script: "go test", Status: ci.RunPending, TriggeredByEvent: ci.EventTypePush,
	})
	is.NoErr(err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ci/runs", nil)
	rr := httptest.NewRecorder()
	env.router.ServeHTTP(rr, req)

	is.Equal(rr.Code, http.StatusOK)
	var got []map[string]any
	is.NoErr(json.NewDecoder(rr.Body).Decode(&got))
	is.Equal(len(got), 1)
	is.Equal(got[0]["workflow_name"], "unit")
}

// stubs ---------------------------------------------------------------

func newStubSource() *stubSource { return &stubSource{} }

type stubSource struct{}

func (s *stubSource) ParseMagicFolder(_ context.Context, _ string) ([]ci.WorkflowDefinition, error) {
	return nil, nil
}

func (s *stubSource) ParseMagicFolderAtCommit(_ context.Context, _ string, _ string) ([]ci.WorkflowDefinition, error) {
	return nil, nil
}

type stubCIDispatcher struct{}

func (stubCIDispatcher) DispatchRun(_ context.Context, _ ci.RunnerRegistration, _ ci.Run) error {
	return nil
}

func (stubCIDispatcher) CancelRun(_ context.Context, _ ci.RunnerRegistration, _ ci.Run) error {
	return nil
}

type stubCITokens struct{}

func (stubCITokens) NewToken() (string, error) { return "test-token", nil }

type stubCIClock struct{}

func (stubCIClock) Now() time.Time { return time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC) }
