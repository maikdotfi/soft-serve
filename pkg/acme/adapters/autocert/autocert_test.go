package autocert

import (
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
	"testing"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/acme"
	"github.com/charmbracelet/soft-serve/pkg/acme/adapters/fake"
	xautocert "golang.org/x/crypto/acme/autocert"
)

// unreachableCA makes any accidental ACME request fail fast instead of
// reaching Let's Encrypt.
const unreachableCA = "https://127.0.0.1:1/directory"

func newIssuer(t *testing.T, cache acme.Cache) *Issuer {
	t.Helper()
	iss, err := New(acme.Config{Domains: []string{"git.example.com"}, CAURL: unreachableCA}, cache)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	return iss
}

func TestNew_RejectsInvalidConfig(t *testing.T) {
	_, err := New(acme.Config{}, fake.NewCache())
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

func TestCacheBridge_TranslatesMissToAutocert(t *testing.T) {
	_, err := cacheBridge{fake.NewCache()}.Get(context.Background(), "absent")
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
