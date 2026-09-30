package autocert

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/acme"
	"github.com/charmbracelet/soft-serve/pkg/acme/adapters/fake"
	xautocert "golang.org/x/crypto/acme/autocert"
)

// unreachableCA makes any accidental ACME request fail fast instead of
// reaching Let's Encrypt.
const unreachableCA = "https://127.0.0.1:1/directory"

func newIssuer(t *testing.T, cache acme.Cache) *Issuer {
	t.Helper()
	iss, _ := newLoggedIssuer(t, cache)
	return iss
}

// newLoggedIssuer returns an issuer and the buffer it logs to, at debug level.
func newLoggedIssuer(t *testing.T, cache acme.Cache) (*Issuer, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger := log.New(&buf)
	logger.SetLevel(log.DebugLevel)
	iss, err := New(acme.Config{Domains: []string{"git.example.com"}, CAURL: unreachableCA}, cache, logger)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	return iss, &buf
}

// logLines returns the lines of buf containing substr.
func logLines(buf *bytes.Buffer, substr string) []string {
	var out []string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, substr) {
			out = append(out, l)
		}
	}
	return out
}

func TestNew_RejectsInvalidConfig(t *testing.T) {
	_, err := New(acme.Config{}, fake.NewCache(), log.New(&bytes.Buffer{}))
	if !errors.Is(err, acme.ErrNoDomains) {
		t.Fatalf("New() err = %v, want ErrNoDomains", err)
	}
}

func TestTLSConfig_AdvertisesTLSALPNChallengeProtocol(t *testing.T) {
	cfg := newIssuer(t, fake.NewCache()).TLSConfig()
	for _, proto := range []string{"acme-tls/1", "h2", "http/1.1"} {
		if !slices.Contains(cfg.NextProtos, proto) {
			t.Errorf("NextProtos = %v, missing %q", cfg.NextProtos, proto)
		}
	}
}

func TestTLSConfig_RejectsUnconfiguredHost(t *testing.T) {
	cfg := newIssuer(t, fake.NewCache()).TLSConfig()
	_, err := cfg.GetCertificate(&tls.ClientHelloInfo{ServerName: "evil.example.com"})
	if !errors.Is(err, acme.ErrHostNotAllowed) {
		t.Fatalf("GetCertificate() err = %v, want ErrHostNotAllowed", err)
	}
}

func TestTLSConfig_ServesCertificateFromCache(t *testing.T) {
	cache := fake.NewCache()
	pemData, leaf := selfSigned(t, "git.example.com")
	if err := cache.Put(context.Background(), "git.example.com", pemData); err != nil {
		t.Fatal(err)
	}

	srvConn, cliConn := net.Pipe()
	t.Cleanup(func() { srvConn.Close(); cliConn.Close() })
	go tls.Server(srvConn, newIssuer(t, cache).TLSConfig()).Handshake() //nolint: errcheck

	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	cli := tls.Client(cliConn, &tls.Config{ServerName: "git.example.com", RootCAs: roots})
	if err := cli.Handshake(); err != nil {
		t.Fatalf("Handshake() = %v", err)
	}
	if got := cli.ConnectionState().PeerCertificates[0]; !got.Equal(leaf) {
		t.Fatalf("server presented %v, want cached cert", got.Subject)
	}
}

func TestTLSConfig_LogsIssuanceFailureAsError(t *testing.T) {
	iss, buf := newLoggedIssuer(t, fake.NewCache())
	if _, err := iss.TLSConfig().GetCertificate(&tls.ClientHelloInfo{ServerName: "git.example.com"}); err == nil {
		t.Fatal("GetCertificate() = nil error, want failure from unreachable CA")
	}
	lines := logLines(buf, "ERRO")
	if len(lines) != 1 || !strings.Contains(lines[0], "git.example.com") || !strings.Contains(lines[0], "127.0.0.1:1") {
		t.Fatalf("error log lines = %q, want one naming the host and the CA error", lines)
	}
}

func TestTLSConfig_LogsTLSALPNChallenge(t *testing.T) {
	iss, buf := newLoggedIssuer(t, fake.NewCache())
	iss.TLSConfig().GetCertificate(&tls.ClientHelloInfo{ //nolint: errcheck
		ServerName:      "git.example.com",
		SupportedProtos: []string{"acme-tls/1"},
	})
	if lines := logLines(buf, "TLS-ALPN-01 challenge"); len(lines) != 1 || !strings.Contains(lines[0], "INFO") {
		t.Fatalf("log = %q, want one INFO line about the challenge", buf.String())
	}
}

func TestTLSConfig_DoesNotLogUnservableHellos(t *testing.T) {
	tests := []struct {
		name  string
		hello *tls.ClientHelloInfo
	}{
		{"unconfigured host", &tls.ClientHelloInfo{ServerName: "evil.example.com"}},
		{"missing server name", &tls.ClientHelloInfo{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iss, buf := newLoggedIssuer(t, fake.NewCache())
			if _, err := iss.TLSConfig().GetCertificate(tt.hello); err == nil {
				t.Fatal("GetCertificate() = nil error, want rejection")
			}
			if buf.Len() != 0 {
				t.Fatalf("log = %q, want nothing", buf.String())
			}
		})
	}
}

func TestCacheBridge_LogsStoredCertificates(t *testing.T) {
	tests := []struct {
		key     string
		wantLog bool
	}{
		{"git.example.com", true},
		{"git.example.com+rsa", true},
		{"acme_account+key", false},
		{"git.example.com+token", false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			iss, buf := newLoggedIssuer(t, fake.NewCache())
			if err := iss.m.Cache.Put(context.Background(), tt.key, []byte("x")); err != nil {
				t.Fatal(err)
			}
			if got := len(logLines(buf, "certificate stored")) == 1; got != tt.wantLog {
				t.Fatalf("logged = %v, want %v; log = %q", got, tt.wantLog, buf.String())
			}
		})
	}
}

func TestCacheBridge_TranslatesMissToAutocert(t *testing.T) {
	_, err := cacheBridge{c: fake.NewCache()}.Get(context.Background(), "absent")
	if !errors.Is(err, xautocert.ErrCacheMiss) {
		t.Fatalf("Get() err = %v, want autocert.ErrCacheMiss", err)
	}
}

// selfSigned returns a cert for host in autocert's cache format (private
// key PEM followed by the certificate PEM) and the parsed certificate.
func selfSigned(t *testing.T, host string) ([]byte, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: host},
		DNSNames:              []string{host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(90 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	return out, leaf
}
