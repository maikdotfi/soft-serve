package softserveadapter_test

import (
	"context"
	"errors"
	"io"
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
	"github.com/charmbracelet/soft-serve/pkg/webui/adminauth"
	"github.com/charmbracelet/soft-serve/pkg/webui/adminauth/adminauthtest"
	"github.com/charmbracelet/soft-serve/pkg/webui/adminauth/softserveadapter"
)

func TestAdapter_SatisfiesContract(t *testing.T) {
	ctx, be := newBackend(t)
	adminToken := createToken(t, ctx, be, "admin", time.Time{})
	if _, err := be.CreateUser(ctx, "bob", proto.UserOptions{}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	bobToken := createToken(t, ctx, be, "bob", time.Time{})

	adminauthtest.Run(t, softserveadapter.New(be), adminauthtest.Fixture{
		AdminUsername: "admin", AdminToken: adminToken,
		UserUsername: "bob", UserToken: bobToken,
	})
}

func TestAdapter_ExpiredTokenIsInvalid(t *testing.T) {
	ctx, be := newBackend(t)
	token := createToken(t, ctx, be, "admin", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))

	err := softserveadapter.New(be).Authenticate(ctx, "admin", token)
	if !errors.Is(err, adminauth.ErrInvalidCredentials) {
		t.Fatalf("Authenticate = %v, want ErrInvalidCredentials", err)
	}
}

func createToken(t *testing.T, ctx context.Context, be *backend.Backend, username string, expiresAt time.Time) string {
	t.Helper()
	u, err := be.User(ctx, username)
	if err != nil {
		t.Fatalf("User(%q): %v", username, err)
	}
	token, err := be.CreateAccessToken(ctx, u, "ui", expiresAt)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	return token
}

func newBackend(t *testing.T) (context.Context, *backend.Backend) {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{
		DataPath: tmp,
		DB: config.DBConfig{
			Driver:     "sqlite",
			DataSource: filepath.Join(tmp, "test.db") + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)",
		},
	}
	ctx := config.WithContext(context.Background(), cfg)
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
	return ctx, backend.New(ctx, cfg, dbx, database.New(ctx, dbx))
}
