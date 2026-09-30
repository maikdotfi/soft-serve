// Package fake provides an in-memory adminauth.Authenticator for tests.
package fake

import (
	"context"

	"github.com/charmbracelet/soft-serve/pkg/webui/adminauth"
)

// User is a user known to the fake, with a single access token.
type User struct {
	Username string
	Token    string
	Admin    bool
}

// Authenticator is an in-memory adminauth.Authenticator.
type Authenticator struct {
	byToken map[string]User
}

var _ adminauth.Authenticator = (*Authenticator)(nil)

// New returns an Authenticator that knows users.
func New(users ...User) *Authenticator {
	a := &Authenticator{byToken: make(map[string]User, len(users))}
	for _, u := range users {
		a.byToken[u.Token] = u
	}
	return a
}

// Authenticate implements adminauth.Authenticator.
func (a *Authenticator) Authenticate(_ context.Context, username, secret string) error {
	u, ok := a.byToken[secret]
	if username == "" || secret == "" || !ok || u.Username != username {
		return adminauth.ErrInvalidCredentials
	}
	if !u.Admin {
		return adminauth.ErrNotAdmin
	}
	return nil
}
