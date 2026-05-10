// Package backendaccess provides a ci.RepoAccessChecker implementation
// that delegates to a *backend.Backend for per-repo ACL checks.
package backendaccess

import (
	"context"

	"github.com/charmbracelet/soft-serve/pkg/access"
	"github.com/charmbracelet/soft-serve/pkg/backend"
)

// Checker is a ci.RepoAccessChecker backed by *backend.Backend.
type Checker struct {
	be *backend.Backend
}

// New returns a Checker that delegates to the given Backend.
// The returned Checker is safe for concurrent use.
func New(be *backend.Backend) *Checker {
	return &Checker{be: be}
}

// CanWriteToRepo returns true when the user has at least read-write
// access to the repository. An empty username is treated as an
// anonymous user, which will never have write access to a private
// repo; public repos may grant write depending on the backend's
// AnonAccess configuration.
func (c *Checker) CanWriteToRepo(ctx context.Context, username, repoName string) (bool, error) {
	level := c.be.AccessLevel(ctx, repoName, username)
	return level >= access.ReadWriteAccess, nil
}
