package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"strings"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/acme"
	"github.com/charmbracelet/soft-serve/pkg/acme/adapters/autocert"
	"github.com/charmbracelet/soft-serve/pkg/acme/adapters/dircache"
	"github.com/charmbracelet/soft-serve/pkg/config"
)

// ErrACMEWithStaticTLS is returned when both ACME and a static TLS
// certificate are configured for the HTTP server.
var ErrACMEWithStaticTLS = errors.New("http.acme cannot be combined with http.tls_cert_path/tls_key_path")

// WireACME configures srv to serve Let's Encrypt (or other ACME)
// certificates when cfg.HTTP.ACME is enabled. It is the single call
// cmd/soft/serve makes into fork code for automatic TLS.
func WireACME(ctx context.Context, srv interface{ SetTLSConfig(*tls.Config) }) error {
	cfg := config.FromContext(ctx)
	ac := cfg.HTTP.ACME
	if !ac.Enabled {
		return nil
	}
	if cfg.HTTP.TLSCertPath != "" || cfg.HTTP.TLSKeyPath != "" {
		return ErrACMEWithStaticTLS
	}

	logger := log.FromContext(ctx).WithPrefix("acme")
	iss, err := autocert.New(acme.Config{
		Domains: ac.Domains,
		Email:   ac.Email,
		CAURL:   ac.CAURL,
	}, dircache.New(ac.CacheDir(cfg.DataPath)), logger)
	if err != nil {
		return err
	}
	srv.SetTLSConfig(iss.TLSConfig())

	logger.Info("automatic TLS enabled", "domains", ac.Domains, "cache", ac.CacheDir(cfg.DataPath))
	if !strings.HasPrefix(cfg.HTTP.PublicURL, "https://") {
		logger.Warn("http.public_url is not https; clone URLs will be wrong", "public_url", cfg.HTTP.PublicURL)
	}
	return nil
}
