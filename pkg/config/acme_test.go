package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matryer/is"
)

func TestParseEnv_ReadsACMEConfig(t *testing.T) {
	is := is.New(t)
	t.Setenv("SOFT_SERVE_HTTP_ACME_ENABLED", "true")
	t.Setenv("SOFT_SERVE_HTTP_ACME_DOMAINS", "git.example.com,code.example.com")
	t.Setenv("SOFT_SERVE_HTTP_ACME_EMAIL", "ops@example.com")
	t.Setenv("SOFT_SERVE_HTTP_ACME_CA_URL", "https://acme-staging-v02.api.letsencrypt.org/directory")

	cfg := DefaultConfig()
	is.NoErr(cfg.ParseEnv())
	is.Equal(cfg.HTTP.ACME, ACMEConfig{
		Enabled: true,
		Domains: []string{"git.example.com", "code.example.com"},
		Email:   "ops@example.com",
		CAURL:   "https://acme-staging-v02.api.letsencrypt.org/directory",
	})
}

func TestParseFile_ReadsACMEConfig(t *testing.T) {
	is := is.New(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	is.NoErr(os.WriteFile(path, []byte("http:\n  acme:\n    enabled: true\n    domains: [git.example.com]\n"), 0o600))

	cfg := DefaultConfig()
	is.NoErr(parseFile(cfg, path))
	is.True(cfg.HTTP.ACME.Enabled)
	is.Equal(cfg.HTTP.ACME.Domains, []string{"git.example.com"})
}

func TestACMEConfig_CacheDir(t *testing.T) {
	tests := []struct {
		name      string
		cachePath string
		want      string
	}{
		{"defaults to acme under the data path", "", "/data/acme"},
		{"resolves a relative path against the data path", "certs", "/data/certs"},
		{"keeps an absolute path", "/var/cache/acme", "/var/cache/acme"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ACMEConfig{CachePath: tt.cachePath}.CacheDir("/data")
			if got != tt.want {
				t.Fatalf("CacheDir() = %q, want %q", got, tt.want)
			}
		})
	}
}
