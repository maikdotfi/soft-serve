//go:build integration

// Package garagetest is a test helper for integration tests that target a
// locally-running Garage daemon (see `make garage-up` at the repo root).
//
// The daemon itself is managed externally; this package only creates a fresh,
// uniquely-named bucket per call and tears it down at the end of the test.
// Connection details are read from the GARAGE_* environment variables that
// `make garage-up` writes to .garage/garage.env.
//
// Usage:
//
//	cfg := garagetest.RequireBucket(t)
//	adapter, err := s3.NewAdapter(cfg)
//	// ... exercise adapter against a real S3-compatible target ...
package garagetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	s3adapter "github.com/charmbracelet/soft-serve/pkg/backup/adapters/s3"
)

const (
	envEndpoint  = "GARAGE_S3_ENDPOINT"
	envRegion    = "GARAGE_S3_REGION"
	envAccessKey = "GARAGE_ACCESS_KEY"
	envSecretKey = "GARAGE_SECRET_KEY"
)

const skipMessage = "Garage integration test skipped: " +
	"run 'make garage-up' at the repo root, then 'make test-integration' " +
	"(or eval $(cat .garage/garage.env) before 'go test -tags integration')"

// RequireBucket returns an s3.S3Config pointed at the running Garage daemon,
// with a freshly created, uniquely named bucket that is deleted (best-effort)
// when the test ends.
//
// The test is t.Skip'd if the GARAGE_* env vars aren't set. It is t.Fatal'd
// if the env is set but the daemon is unreachable or the bucket cannot be
// created — the helper deliberately does not paper over a misconfigured
// environment.
func RequireBucket(t *testing.T) s3adapter.S3Config {
	t.Helper()

	endpoint := stripScheme(strings.TrimSpace(os.Getenv(envEndpoint)))
	access := strings.TrimSpace(os.Getenv(envAccessKey))
	secret := strings.TrimSpace(os.Getenv(envSecretKey))
	region := strings.TrimSpace(os.Getenv(envRegion))
	if endpoint == "" || access == "" || secret == "" {
		t.Skip(skipMessage)
	}
	if region == "" {
		region = "garage"
	}

	bucket := "soft-serve-it-" + randHex(t, 6)

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(access, secret, ""),
		Region: region,
		Secure: false,
	})
	if err != nil {
		t.Fatalf("garagetest: building minio client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region}); err != nil {
		t.Fatalf("garagetest: creating bucket %q on %s: %v", bucket, endpoint, err)
	}

	t.Cleanup(func() { cleanupBucket(t, client, bucket) })

	return s3adapter.S3Config{
		Endpoint:  endpoint,
		Region:    region,
		Bucket:    bucket,
		AccessKey: access,
		SecretKey: secret,
		Secure:    false,
	}
}

func cleanupBucket(t *testing.T, client *minio.Client, bucket string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	objCh := client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true})
	for e := range client.RemoveObjects(ctx, bucket, objCh, minio.RemoveObjectsOptions{}) {
		if e.Err != nil {
			t.Logf("garagetest: removing %q: %v", e.ObjectName, e.Err)
		}
	}
	if err := client.RemoveBucket(ctx, bucket); err != nil {
		t.Logf("garagetest: removing bucket %q: %v (leaked)", bucket, err)
	}
}

func randHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("garagetest: rand: %v", err)
	}
	return hex.EncodeToString(b)
}

// stripScheme accepts either "host:port" or a full URL ("http://host:port") so
// the helper is forgiving about how GARAGE_S3_ENDPOINT is written; minio-go
// itself wants just "host:port".
func stripScheme(endpoint string) string {
	if !strings.Contains(endpoint, "://") {
		return endpoint
	}
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return endpoint
}
