package webui_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/webui"
	"github.com/charmbracelet/soft-serve/pkg/webui/adminauth"
	authfake "github.com/charmbracelet/soft-serve/pkg/webui/adminauth/fake"
	"github.com/charmbracelet/soft-serve/pkg/webui/repobrowser/fake"
)

func newAuthedHandler(t *testing.T, a adminauth.Authenticator) http.Handler {
	t.Helper()
	h, err := webui.NewHandler(fake.New(nil), webui.WithAuthenticator(a))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h
}

func TestAuth_BasicCredentials(t *testing.T) {
	a := authfake.New(
		authfake.User{Username: "admin", Token: "admin-token", Admin: true},
		authfake.User{Username: "bob", Token: "bob-token"},
	)
	tests := []struct {
		name       string
		path       string
		setAuth    func(r *http.Request)
		wantStatus int
		wantPrompt bool
	}{
		{"no credentials prompts for login", "/", func(*http.Request) {}, http.StatusUnauthorized, true},
		{"static assets also require login", "/static/style.css", func(*http.Request) {}, http.StatusUnauthorized, true},
		{"wrong token prompts again", "/", func(r *http.Request) { r.SetBasicAuth("admin", "nope") }, http.StatusUnauthorized, true},
		{"non-admin is forbidden", "/", func(r *http.Request) { r.SetBasicAuth("bob", "bob-token") }, http.StatusForbidden, false},
		{"non-basic scheme prompts for login", "/", func(r *http.Request) { r.Header.Set("Authorization", "Token admin-token") }, http.StatusUnauthorized, true},
		{"admin token is let through", "/", func(r *http.Request) { r.SetBasicAuth("admin", "admin-token") }, http.StatusOK, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newAuthedHandler(t, a)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			tt.setAuth(req)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body:\n%s", rec.Code, tt.wantStatus, rec.Body)
			}
			prompt := rec.Header().Get("WWW-Authenticate")
			if tt.wantPrompt && prompt != `Basic realm="Soft Serve", charset="UTF-8"` {
				t.Fatalf("WWW-Authenticate = %q", prompt)
			}
			if !tt.wantPrompt && prompt != "" {
				t.Fatalf("unexpected WWW-Authenticate = %q", prompt)
			}
		})
	}
}

type failingAuth struct{}

func (failingAuth) Authenticate(_ context.Context, _, _ string) error {
	return errors.New("db down")
}

func TestAuth_BackendErrorIs500NotPrompt(t *testing.T) {
	h := newAuthedHandler(t, failingAuth{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "admin-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("unexpected WWW-Authenticate = %q", got)
	}
}
