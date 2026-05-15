package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/config"
	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/db/migrate"
	"github.com/charmbracelet/soft-serve/pkg/proto"
	"github.com/charmbracelet/soft-serve/pkg/store/database"
	"github.com/charmbracelet/soft-serve/pkg/workitem"
	"github.com/charmbracelet/soft-serve/pkg/workitem/adapters/memstore"
	"github.com/gorilla/mux"
)

type workItemAPITestEnv struct {
	ctx    context.Context
	router http.Handler
	svc    *workitem.Service
	token  string
}

func newWorkItemAPITestEnv(t *testing.T) *workItemAPITestEnv {
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
	ctx = config.WithContext(ctx, cfg)
	ctx = log.WithContext(ctx, log.New(io.Discard))

	dbx, err := db.Open(ctx, cfg.DB.Driver, cfg.DB.DataSource)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { dbx.Close() })
	ctx = db.WithContext(ctx, dbx)
	if err := migrate.Migrate(ctx, dbx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	dbstore := database.New(ctx, dbx)
	be := backend.New(ctx, cfg, dbx, dbstore)
	ctx = backend.WithContext(ctx, be)

	user, err := be.CreateUser(ctx, "taskadmin", proto.UserOptions{Admin: true})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := be.CreateRepository(ctx, "alpha", user, proto.RepositoryOptions{}); err != nil {
		t.Fatalf("CreateRepository: %v", err)
	}
	token, err := be.CreateAccessToken(ctx, user, "tasks", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	svc := workitem.NewService(memstore.New(), fixedWorkItemAPIClock{now: time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)})
	be.SetWorkItemService(svc)

	router := mux.NewRouter()
	WorkItemController(ctx, router)
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		router.ServeHTTP(w, r.WithContext(ctx))
	})

	return &workItemAPITestEnv{ctx: ctx, router: wrapped, svc: svc, token: token}
}

func TestWorkItemAPI_CreateListAndMove(t *testing.T) {
	env := newWorkItemAPITestEnv(t)

	createBody := bytes.NewBufferString(`{"title":"Build task board","description":"API-backed swimlanes"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos/alpha/work-items", createBody)
	req.Header.Set("Authorization", "Token "+env.token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body:\n%s", rec.Code, rec.Body.String())
	}
	var created workItemDTO
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID == 0 || created.Lane != "backlog" || created.Title != "Build task board" {
		t.Fatalf("created = %#v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/repos/alpha/work-items", nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body:\n%s", rec.Code, rec.Body.String())
	}
	var listed []workItemDTO
	if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed = %#v", listed)
	}

	moveBody := bytes.NewBufferString(`{"lane":"wip"}`)
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/repos/alpha/work-items/1", moveBody)
	req.Header.Set("Authorization", "Token "+env.token)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("move status = %d, body:\n%s", rec.Code, rec.Body.String())
	}
	var moved workItemDTO
	if err := json.NewDecoder(rec.Body).Decode(&moved); err != nil {
		t.Fatalf("decode move: %v", err)
	}
	if moved.Lane != "wip" {
		t.Fatalf("moved lane = %q, want wip", moved.Lane)
	}
}

func TestWorkItemAPI_CreateRequiresWriteAccess(t *testing.T) {
	env := newWorkItemAPITestEnv(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos/alpha/work-items", bytes.NewBufferString(`{"title":"No token"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestWorkItemAPI_MoveRejectsInvalidLane(t *testing.T) {
	env := newWorkItemAPITestEnv(t)
	if _, err := env.svc.Create(env.ctx, "alpha", "Move me", ""); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/repos/alpha/work-items/1", bytes.NewBufferString(`{"lane":"review"}`))
	req.Header.Set("Authorization", "Token "+env.token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body:\n%s", rec.Code, rec.Body.String())
	}
}

type fixedWorkItemAPIClock struct {
	now time.Time
}

func (c fixedWorkItemAPIClock) Now() time.Time {
	return c.now
}
