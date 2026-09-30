// Package autocert is the ACME adapter backed by
// golang.org/x/crypto/acme/autocert. It answers TLS-ALPN-01 challenges on
// the TLS listener itself, so the server must be reachable on port 443.
package autocert

import (
	"context"
	"crypto/tls"
	"errors"
	"slices"
	"strings"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/acme"
	xacme "golang.org/x/crypto/acme"
	xautocert "golang.org/x/crypto/acme/autocert"
)

// Issuer obtains, caches and renews certificates for the configured domains.
type Issuer struct {
	m      *xautocert.Manager
	logger *log.Logger
}

// New returns an Issuer for cfg that stores its state in cache and logs
// certificate activity to logger.
func New(cfg acme.Config, cache acme.Cache, logger *log.Logger) (*Issuer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	m := &xautocert.Manager{
		Prompt:     xautocert.AcceptTOS,
		Email:      cfg.Email,
		HostPolicy: acme.HostPolicy(cfg.Domains),
		Cache:      cacheBridge{c: cache, logger: logger},
	}
	if cfg.CAURL != "" {
		m.Client = &xacme.Client{DirectoryURL: cfg.CAURL}
	}
	return &Issuer{m: m, logger: logger}, nil
}

// TLSConfig returns a TLS config that serves the managed certificates and
// answers TLS-ALPN-01 challenges.
func (i *Issuer) TLSConfig() *tls.Config {
	cfg := i.m.TLSConfig()
	cfg.GetCertificate = i.getCertificate
	return cfg
}

// getCertificate wraps the manager's GetCertificate with logging. Hellos
// without SNI or for unconfigured hosts are not logged; scanners send them
// constantly.
func (i *Issuer) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if slices.Contains(hello.SupportedProtos, xacme.ALPNProto) {
		i.logger.Info("answering TLS-ALPN-01 challenge", "host", hello.ServerName)
	}
	cert, err := i.m.GetCertificate(hello)
	if err != nil && hello.ServerName != "" && !errors.Is(err, acme.ErrHostNotAllowed) {
		i.logger.Error("certificate unavailable", "host", hello.ServerName, "err", err)
	}
	return cert, err
}

// cacheBridge adapts acme.Cache to autocert.Cache, translating cache misses
// and logging when a new certificate is stored.
type cacheBridge struct {
	c      acme.Cache
	logger *log.Logger
}

func (b cacheBridge) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := b.c.Get(ctx, key)
	if errors.Is(err, acme.ErrCacheMiss) {
		return nil, xautocert.ErrCacheMiss
	}
	return data, err
}

func (b cacheBridge) Put(ctx context.Context, key string, data []byte) error {
	if err := b.c.Put(ctx, key, data); err != nil {
		return err
	}
	if isCertKey(key) {
		b.logger.Info("certificate stored", "key", key)
	}
	return nil
}

// isCertKey reports whether key holds a certificate: autocert stores ECDSA
// certificates under the bare domain and RSA ones with a "+rsa" suffix.
// Other keys (account key, challenge tokens) contain a different "+" suffix.
func isCertKey(key string) bool {
	return !strings.Contains(key, "+") || strings.HasSuffix(key, "+rsa")
}

func (b cacheBridge) Delete(ctx context.Context, key string) error {
	return b.c.Delete(ctx, key)
}
