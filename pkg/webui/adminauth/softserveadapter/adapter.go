// Package softserveadapter implements adminauth.Authenticator on top of the
// soft-serve backend's users and access tokens.
package softserveadapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/proto"
	"github.com/charmbracelet/soft-serve/pkg/webui/adminauth"
)

// Adapter checks access tokens against a *backend.Backend.
type Adapter struct {
	be *backend.Backend
}

var _ adminauth.Authenticator = (*Adapter)(nil)

// New returns an Adapter backed by be. It panics if be is nil.
func New(be *backend.Backend) *Adapter {
	if be == nil {
		panic("softserveadapter: nil backend")
	}
	return &Adapter{be: be}
}

// Authenticate implements adminauth.Authenticator.
func (a *Adapter) Authenticate(ctx context.Context, username, secret string) error {
	if username == "" || secret == "" {
		return adminauth.ErrInvalidCredentials
	}
	u, err := a.be.UserByAccessToken(ctx, secret)
	switch {
	case errors.Is(err, proto.ErrUserNotFound), errors.Is(err, proto.ErrTokenExpired):
		return adminauth.ErrInvalidCredentials
	case err != nil:
		return fmt.Errorf("softserveadapter: lookup access token: %w", err)
	}
	if u.Username() != username {
		return adminauth.ErrInvalidCredentials
	}
	if !u.IsAdmin() {
		return adminauth.ErrNotAdmin
	}
	return nil
}
