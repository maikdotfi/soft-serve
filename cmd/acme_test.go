package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"slices"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/acme"
	"github.com/charmbracelet/soft-serve/pkg/config"
)

type tlsRecorder struct{ cfg *tls.Config }

func (r *tlsRecorder) SetTLSConfig(c *tls.Config) { r.cfg = c }

func acmeCtx(t *testing.T, mutate func(*config.Config)) context.Context {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.DataPath = t.TempDir()
	mutate(cfg)
	return config.WithContext(context.Background(), cfg)
}

func TestWireACME_DisabledLeavesTLSUntouched(t *testing.T) {
	var rec tlsRecorder
	if err := WireACME(acmeCtx(t, func(*config.Config) {}), &rec); err != nil {
		t.Fatalf("WireACME() = %v", err)
	}
	if rec.cfg != nil {
		t.Fatal("WireACME() set a TLS config while ACME is disabled")
	}
}

func TestWireACME_EnabledSetsChallengeCapableTLSConfig(t *testing.T) {
	var rec tlsRecorder
	ctx := acmeCtx(t, func(c *config.Config) {
		c.HTTP.ACME = config.ACMEConfig{Enabled: true, Domains: []string{"git.example.com"}}
	})
	if err := WireACME(ctx, &rec); err != nil {
		t.Fatalf("WireACME() = %v", err)
	}
	if rec.cfg == nil || !slices.Contains(rec.cfg.NextProtos, "acme-tls/1") {
		t.Fatalf("TLS config = %+v, want one that answers TLS-ALPN-01", rec.cfg)
	}
}

func TestWireACME_RejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.Config)
		want   error
	}{
		{
			name: "no domains",
			mutate: func(c *config.Config) {
				c.HTTP.ACME = config.ACMEConfig{Enabled: true}
			},
			want: acme.ErrNoDomains,
		},
		{
			name: "static TLS cert also configured",
			mutate: func(c *config.Config) {
				c.HTTP.ACME = config.ACMEConfig{Enabled: true, Domains: []string{"git.example.com"}}
				c.HTTP.TLSCertPath, c.HTTP.TLSKeyPath = "cert.pem", "key.pem"
			},
			want: ErrACMEWithStaticTLS,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rec tlsRecorder
			if err := WireACME(acmeCtx(t, tt.mutate), &rec); !errors.Is(err, tt.want) {
				t.Fatalf("WireACME() = %v, want %v", err, tt.want)
			}
		})
	}
}
