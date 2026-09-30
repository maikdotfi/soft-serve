// Package adminauthtest holds the contract every adminauth.Authenticator
// must satisfy.
package adminauthtest

import (
	"context"
	"errors"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/webui/adminauth"
)

// Fixture describes the credentials an adapter under test was seeded with.
type Fixture struct {
	AdminUsername string
	AdminToken    string
	UserUsername  string // a non-admin user
	UserToken     string
}

// Run exercises a against f.
func Run(t *testing.T, a adminauth.Authenticator, f Fixture) {
	t.Helper()
	ctx := context.Background()
	tests := []struct {
		name     string
		username string
		secret   string
		want     error
	}{
		{"admin token accepted", f.AdminUsername, f.AdminToken, nil},
		{"non-admin token rejected as not admin", f.UserUsername, f.UserToken, adminauth.ErrNotAdmin},
		{"unknown token rejected", f.AdminUsername, "not-a-token", adminauth.ErrInvalidCredentials},
		{"token of another user rejected", f.UserUsername, f.AdminToken, adminauth.ErrInvalidCredentials},
		{"empty secret rejected", f.AdminUsername, "", adminauth.ErrInvalidCredentials},
		{"empty username rejected", "", f.AdminToken, adminauth.ErrInvalidCredentials},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := a.Authenticate(ctx, tt.username, tt.secret)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("Authenticate = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("Authenticate = %v, want %v", err, tt.want)
			}
		})
	}
}
