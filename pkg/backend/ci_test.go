package backend

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/ci"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/memstore"
	"github.com/charmbracelet/soft-serve/pkg/hooks"
	"github.com/charmbracelet/soft-serve/pkg/store/database"
	"github.com/charmbracelet/soft-serve/pkg/webhook"
	"github.com/matryer/is"
)

// TestBackend_CIService_NotWiredByDefault verifies that the CI service
// is nil until SetCIService is called, mirroring the backup-service
// guard pattern. PostReceive must not panic in this state.
func TestBackend_CIService_NotWiredByDefault(t *testing.T) {
	is := is.New(t)
	ctx, dbx, cfg := testContext(t)

	be := New(ctx, cfg, dbx, database.New(ctx, dbx))

	is.True(be.CIService() == nil) // not yet wired
}

// TestBackend_SetCIService_WiresService verifies that after wiring,
// CIService() returns the registered service.
func TestBackend_SetCIService_WiresService(t *testing.T) {
	is := is.New(t)
	ctx, dbx, cfg := testContext(t)

	be := New(ctx, cfg, dbx, database.New(ctx, dbx))
	svc := newTestCIService()
	be.SetCIService(svc)

	is.True(be.CIService() != nil)
	is.Equal(be.CIService(), svc)
}

func newTestCIService() *ci.Service {
	return newTestCIServiceWith(memstore.New(), &recordingWorkflowSource{})
}

func newTestCIServiceWith(store ci.Store, ws ci.WorkflowSource) *ci.Service {
	return ci.NewService(
		ci.DefaultConfig(),
		store,
		ws,
		stubDispatcher{},
		stubTokens{},
		stubClock{},
		nil,
	)
}

// TestBackend_CIPreReceive_NilServiceIsNoop ensures that with the CI
// subsystem disabled (no SetCIService call), the push gate does
// nothing and accepts every push.
func TestBackend_CIPreReceive_NilServiceIsNoop(t *testing.T) {
	is := is.New(t)
	ctx, dbx, cfg := testContext(t)
	be := New(ctx, cfg, dbx, database.New(ctx, dbx))

	var stderr bytes.Buffer
	err := be.CIPreReceive(ctx, "any-repo", []hooks.HookArg{
		{RefName: "refs/heads/main", OldSha: "0000", NewSha: "abcd"},
	}, &stderr)
	is.NoErr(err)
	is.Equal(stderr.Len(), 0)
}

// TestBackend_CIPreReceive_RejectsPushOnParseError verifies that if
// the WorkflowSource reports a parse error against the incoming
// commit SHA (the new tree), CIPreReceive returns the error and
// writes a human-readable message to stderr so the git client sees
// it on rejection. This is the gate behavior from RepoPushGate in
// ci.allium.
func TestBackend_CIPreReceive_RejectsPushOnParseError(t *testing.T) {
	is := is.New(t)
	ctx, dbx, cfg := testContext(t)
	be := New(ctx, cfg, dbx, database.New(ctx, dbx))

	ws := &recordingWorkflowSource{commitParseErr: ci.ErrWorkflowParse}
	be.SetCIService(newTestCIServiceWith(memstore.New(), ws))

	var stderr bytes.Buffer
	err := be.CIPreReceive(ctx, "repo1", []hooks.HookArg{
		{RefName: "refs/heads/main", OldSha: "0000000000000000000000000000000000000000", NewSha: "deadbeef"},
	}, &stderr)
	is.True(err != nil)              // parse failure must propagate
	is.True(stderr.Len() > 0)        // human-readable rejection message
}

// TestBackend_CIPreReceive_AcceptsPushWhenWorkflowsParse ensures the
// gate is silent on success.
func TestBackend_CIPreReceive_AcceptsPushWhenWorkflowsParse(t *testing.T) {
	is := is.New(t)
	ctx, dbx, cfg := testContext(t)
	be := New(ctx, cfg, dbx, database.New(ctx, dbx))

	ws := &recordingWorkflowSource{} // no error
	be.SetCIService(newTestCIServiceWith(memstore.New(), ws))

	var stderr bytes.Buffer
	err := be.CIPreReceive(ctx, "repo1", []hooks.HookArg{
		{RefName: "refs/heads/main", OldSha: "0000000000000000000000000000000000000000", NewSha: "deadbeef"},
	}, &stderr)
	is.NoErr(err)
	is.Equal(stderr.Len(), 0)
	is.Equal(len(ws.commitParseCalls), 1)               // validated against new tree
	is.Equal(ws.commitParseCalls[0].CommitSHA, "deadbeef")
}

// TestBackend_OnWebhookFired_NilServiceIsNoop verifies that with no
// CI service wired, the in-process webhook subscriber simply drops
// events on the floor (no panic, no work).
func TestBackend_OnWebhookFired_NilServiceIsNoop(t *testing.T) {
	is := is.New(t)
	ctx, dbx, cfg := testContext(t)
	be := New(ctx, cfg, dbx, database.New(ctx, dbx))

	// We can't easily build a webhook.PushEvent without a *proto.Repository,
	// so use a hand-rolled stub.
	payload := webhookPayloadStub{event: webhook.EventPush, repoName: "repo1"}
	be.OnWebhookFired(ctx, payload)
	is.True(true) // no panic
}

// TestBackend_OnWebhookFired_FansOutToCIService verifies that when a
// CI service is wired, OnWebhookFired translates the webhook.Event
// to a ci.EventType and creates pending Runs for matching workflows
// (rule CreateRunsOnEvent in ci.allium).
func TestBackend_OnWebhookFired_FansOutToCIService(t *testing.T) {
	is := is.New(t)
	ctx, dbx, cfg := testContext(t)
	be := New(ctx, cfg, dbx, database.New(ctx, dbx))

	store := memstore.New()
	svc := newTestCIServiceWith(store, &recordingWorkflowSource{})
	be.SetCIService(svc)

	// Seed a workflow that triggers on push.
	is.NoErr(store.UpsertWorkflow(ctx, ci.Workflow{
		RepoName: "repo1",
		Name:     "unit",
		Script:   "go test ./...",
		RunsOn:   "linux-amd64",
		Triggers: map[ci.EventType]bool{ci.EventTypePush: true},
	}))

	be.OnWebhookFired(ctx, webhookPayloadStub{event: webhook.EventPush, repoName: "repo1"})

	runs, err := store.ListRuns(ctx)
	is.NoErr(err)
	is.Equal(len(runs), 1)
	is.Equal(runs[0].WorkflowName, "unit")
	is.Equal(runs[0].Status, ci.RunPending)
}

// webhookPayloadStub implements webhook.EventPayload with bare
// fields so tests don't need to reach into the proto layer to build
// a real PushEvent.
type webhookPayloadStub struct {
	event    webhook.Event
	repoName string
	repoID   int64
}

func (p webhookPayloadStub) Event() webhook.Event   { return p.event }
func (p webhookPayloadStub) RepositoryID() int64    { return p.repoID }
func (p webhookPayloadStub) RepositoryName() string { return p.repoName }

type recordingWorkflowSource struct {
	defs             []ci.WorkflowDefinition
	commitParseErr   error
	parseErr         error
	parseCalls       []string
	commitParseCalls []recordedCommitParse
}

type recordedCommitParse struct {
	RepoName  string
	CommitSHA string
}

func (s *recordingWorkflowSource) ParseMagicFolder(_ context.Context, repoName string) ([]ci.WorkflowDefinition, error) {
	s.parseCalls = append(s.parseCalls, repoName)
	if s.parseErr != nil {
		return nil, s.parseErr
	}
	return append([]ci.WorkflowDefinition(nil), s.defs...), nil
}

func (s *recordingWorkflowSource) ParseMagicFolderAtCommit(_ context.Context, repoName, commitSHA string) ([]ci.WorkflowDefinition, error) {
	s.commitParseCalls = append(s.commitParseCalls, recordedCommitParse{RepoName: repoName, CommitSHA: commitSHA})
	if s.commitParseErr != nil {
		return nil, s.commitParseErr
	}
	return append([]ci.WorkflowDefinition(nil), s.defs...), nil
}

type stubDispatcher struct{}

func (stubDispatcher) DispatchRun(_ context.Context, _ ci.RunnerRegistration, _ ci.Run) error {
	return nil
}

func (stubDispatcher) CancelRun(_ context.Context, _ ci.RunnerRegistration, _ ci.Run) error {
	return nil
}

type stubTokens struct{}

func (stubTokens) NewToken() (string, error) { return "", nil }

type stubClock struct{}

func (stubClock) Now() time.Time { return time.Time{} }
