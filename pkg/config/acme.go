package config

import "path/filepath"

// ACMEConfig configures automatic TLS certificates from an ACME CA such as
// Let's Encrypt. Certificates are obtained with the TLS-ALPN-01 challenge,
// so the HTTP server must be reachable from the internet on port 443.
type ACMEConfig struct {
	// Enabled turns on automatic certificates for the HTTP server.
	Enabled bool `env:"ENABLED" yaml:"enabled"`

	// Domains are the hostnames to obtain certificates for. Each must
	// resolve to this server.
	Domains []string `env:"DOMAINS" yaml:"domains"`

	// Email is the optional ACME account contact address.
	Email string `env:"EMAIL" yaml:"email"`

	// CAURL is the ACME directory URL. Empty means Let's Encrypt production.
	CAURL string `env:"CA_URL" yaml:"ca_url"`

	// CachePath is where account keys and certificates are stored. A
	// relative path is resolved against the data path. Defaults to "acme".
	CachePath string `env:"CACHE_PATH" yaml:"cache_path"`
}

// CacheDir returns the absolute certificate cache directory.
func (c ACMEConfig) CacheDir(dataPath string) string {
	p := c.CachePath
	if p == "" {
		p = "acme"
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dataPath, p)
}
