// Package adminauth is the port the web UI uses to decide whether a request
// comes from a server admin.
//
// The UI only speaks HTTP Basic auth: the username is a soft-serve username
// and the secret is one of that user's access tokens. Adapters translate the
// check onto a concrete user store.
package adminauth

import (
	"context"
	"errors"
)

// Sentinel errors returned by an Authenticator. Callers match with errors.Is.
var (
	// ErrInvalidCredentials means the username/secret pair does not identify a user.
	ErrInvalidCredentials = errors.New("adminauth: invalid credentials")
	// ErrNotAdmin means the credentials are valid but the user is not an admin.
	ErrNotAdmin = errors.New("adminauth: user is not an admin")
)

// Authenticator verifies that username/secret belong to an admin.
type Authenticator interface {
	// Authenticate returns nil when secret is a valid access token owned by
	// the admin user named username.
	Authenticate(ctx context.Context, username, secret string) error
}
