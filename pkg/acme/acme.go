// Package acme obtains and renews TLS certificates for the HTTP server from
// an ACME certificate authority such as Let's Encrypt.
//
// The domain package defines the ports and policy; the ACME protocol itself
// lives in adapters/autocert, and certificate storage in adapters/dircache.
package acme

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

var (
	// ErrNoDomains is returned when ACME is enabled without any domains.
	ErrNoDomains = errors.New("acme: no domains configured")

	// ErrInvalidDomain is returned for a domain a certificate cannot be
	// issued for over TLS-ALPN-01.
	ErrInvalidDomain = errors.New("acme: invalid domain")

	// ErrHostNotAllowed is returned when a TLS client asks for a host that
	// is not configured.
	ErrHostNotAllowed = errors.New("acme: host not allowed")

	// ErrCacheMiss is returned by a Cache when a key is not present.
	ErrCacheMiss = errors.New("acme: cache miss")
)

// Config describes which certificates to obtain and from where.
type Config struct {
	// Domains are the hostnames to obtain certificates for.
	Domains []string

	// Email is the optional ACME account contact address.
	Email string

	// CAURL is the ACME directory URL. Empty means Let's Encrypt production.
	CAURL string
}

// Validate reports whether a certificate can be issued for every domain.
func (c Config) Validate() error {
	if len(c.Domains) == 0 {
		return ErrNoDomains
	}
	for _, d := range c.Domains {
		if err := validateDomain(normalize(d)); err != nil {
			return fmt.Errorf("%w: %q", err, d)
		}
	}
	return nil
}

func validateDomain(d string) error {
	if d == "" || strings.ContainsAny(d, "*:/ ") || net.ParseIP(d) != nil {
		return ErrInvalidDomain
	}
	return nil
}

// HostPolicy returns a function that allows only the given domains,
// compared case-insensitively and ignoring a trailing dot.
func HostPolicy(domains []string) func(ctx context.Context, host string) error {
	allowed := make(map[string]struct{}, len(domains))
	for _, d := range domains {
		allowed[normalize(d)] = struct{}{}
	}
	return func(_ context.Context, host string) error {
		if _, ok := allowed[normalize(host)]; !ok || host == "" {
			return fmt.Errorf("%w: %q", ErrHostNotAllowed, host)
		}
		return nil
	}
}

func normalize(d string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
}

// Cache stores ACME account keys and certificates between restarts.
// Get returns ErrCacheMiss when the key is not present.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, data []byte) error
	Delete(ctx context.Context, key string) error
}
