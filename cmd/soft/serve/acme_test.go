package serve

import (
	"slices"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/config"
	"github.com/charmbracelet/soft-serve/pkg/db"
)

func TestNewServer_ACMEEnabledServesHTTPOverTLS(t *testing.T) {
	ctx, cfg, be, dbx := setupTestBackendWithDB(t)
	cfg.HTTP.ACME = config.ACMEConfig{Enabled: true, Domains: []string{"git.example.com"}}
	ctx = backend.WithContext(ctx, be)
	ctx = db.WithContext(ctx, dbx)

	srv, err := NewServer(ctx)
	if err != nil {
		t.Fatalf("NewServer() = %v", err)
	}
	tlsCfg := srv.HTTPServer.Server.TLSConfig
	if tlsCfg == nil || !slices.Contains(tlsCfg.NextProtos, "acme-tls/1") {
		t.Fatalf("HTTP TLS config = %+v, want ACME TLS-ALPN config", tlsCfg)
	}
}
