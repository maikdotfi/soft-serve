// Package autocert is the ACME adapter backed by
// golang.org/x/crypto/acme/autocert. It answers TLS-ALPN-01 challenges on
// the TLS listener itself, so the server must be reachable on port 443.
package autocert

import (
	"context"
	"crypto/tls"
	"errors"

	"github.com/charmbracelet/soft-serve/pkg/acme"
	xacme "golang.org/x/crypto/acme"
	xautocert "golang.org/x/crypto/acme/autocert"
)

// Issuer obtains, caches and renews certificates for the configured domains.
type Issuer struct {
	m *xautocert.Manager
}

// New returns an Issuer for cfg that stores its state in cache.
func New(cfg acme.Config, cache acme.Cache) (*Issuer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	m := &xautocert.Manager{
		Prompt:     xautocert.AcceptTOS,
		Email:      cfg.Email,
		HostPolicy: acme.HostPolicy(cfg.Domains),
		Cache:      cacheBridge{cache},
	}
	if cfg.CAURL != "" {
		m.Client = &xacme.Client{DirectoryURL: cfg.CAURL}
	}
	return &Issuer{m: m}, nil
}

// TLSConfig returns a TLS config that serves the managed certificates and
// answers TLS-ALPN-01 challenges.
func (i *Issuer) TLSConfig() *tls.Config {
	return i.m.TLSConfig()
}

// cacheBridge adapts acme.Cache to autocert.Cache, translating cache misses.
type cacheBridge struct {
	c acme.Cache
}

func (b cacheBridge) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := b.c.Get(ctx, key)
	if errors.Is(err, acme.ErrCacheMiss) {
		return nil, xautocert.ErrCacheMiss
	}
	return data, err
}

func (b cacheBridge) Put(ctx context.Context, key string, data []byte) error {
	return b.c.Put(ctx, key, data)
}

func (b cacheBridge) Delete(ctx context.Context, key string) error {
	return b.c.Delete(ctx, key)
}
