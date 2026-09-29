package testscript

// Fork-specific testscript wiring. script_test.go references these with
// single "// fork" lines so upstream changes there rebase cleanly.

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rogpeppe/go-internal/testscript"
)

func forkCondition(cond string) (bool, error) {
	// `[env:VAR]` is true iff the named env var is non-empty in the
	// test process. Used by integration scenarios that should skip
	// when prerequisites (e.g. a running Garage daemon) are absent.
	if strings.HasPrefix(cond, "env:") {
		return os.Getenv(strings.TrimPrefix(cond, "env:")) != "", nil
	}
	return false, fmt.Errorf("unknown condition %q", cond)
}

func forkSetupEnv(e *testscript.Env) {
	// Forward GARAGE_* connection details so scenarios that opt into
	// S3 backup verification can reach the running Garage daemon
	// (see Makefile's `garage-up` target). Scenarios skip themselves
	// if these are unset.
	for _, env := range []string{
		"GARAGE_S3_ENDPOINT",
		"GARAGE_S3_REGION",
		"GARAGE_ACCESS_KEY",
		"GARAGE_SECRET_KEY",
	} {
		if v, ok := os.LookupEnv(env); ok {
			e.Setenv(env, v)
		}
	}
}

// cmdS3PrepareBucket creates the S3 bucket named by SOFT_SERVE_BACKUP_BUCKET
// on the configured backup endpoint, idempotently. Used by integration
// scenarios that point soft-serve at a real S3-compatible target (Garage)
// for scheduled backup verification.
func cmdS3PrepareBucket(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) != 0 {
		ts.Fatalf("usage: s3preparebucket")
	}
	client, bucket, region := s3ClientFromEnv(ts)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exists, err := client.BucketExists(ctx, bucket)
	check(ts, err, neg)
	if !exists {
		check(ts, client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region}), neg)
	}
}

// cmdS3BackupWait polls the configured S3 bucket for a repo backup bundle
// for the given repo. It looks under
// "<SOFT_SERVE_BACKUP_PATH_PREFIX>/repos/<repo>/" for at least one .bundle
// object. The optional second argument is a Go duration (e.g. "90s") that
// overrides the default 30s deadline; the schedule-driven scenario uses
// this to absorb the cron poll cadence (~1m) plus upload latency.
func cmdS3BackupWait(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) < 1 || len(args) > 2 {
		ts.Fatalf("usage: s3backupwait <repo> [timeout]")
	}
	repo := args[0]
	timeout := 30 * time.Second
	if len(args) == 2 {
		d, err := time.ParseDuration(args[1])
		if err != nil {
			ts.Fatalf("s3backupwait: parse timeout %q: %v", args[1], err)
		}
		timeout = d
	}
	client, bucket, _ := s3ClientFromEnv(ts)
	prefix := strings.TrimSuffix(ts.Getenv("SOFT_SERVE_BACKUP_PATH_PREFIX"), "/")
	if prefix == "" {
		prefix = "soft-serve"
	}
	objPrefix := prefix + "/repos/" + repo + "/"

	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		found, err := s3HasBundle(ctx, client, bucket, objPrefix)
		cancel()
		if err == nil && found {
			if neg {
				ts.Fatalf("s3backupwait: did not expect a bundle under %s but found one", objPrefix)
			}
			return
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	if neg {
		// Negation succeeds: no bundle appeared within the deadline.
		return
	}
	if lastErr != nil {
		ts.Fatalf("s3backupwait: timed out waiting for bundle under %s: %v", objPrefix, lastErr)
	}
	ts.Fatalf("s3backupwait: timed out waiting for bundle under %s (no objects appeared)", objPrefix)
}

func s3HasBundle(ctx context.Context, client *minio.Client, bucket, prefix string) (bool, error) {
	for obj := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return false, obj.Err
		}
		if strings.HasSuffix(obj.Key, ".bundle") {
			return true, nil
		}
	}
	return false, nil
}

func s3ClientFromEnv(ts *testscript.TestScript) (*minio.Client, string, string) {
	endpoint := stripScheme(strings.TrimSpace(ts.Getenv("SOFT_SERVE_BACKUP_ENDPOINT")))
	access := strings.TrimSpace(ts.Getenv("SOFT_SERVE_BACKUP_ACCESS_KEY"))
	secret := strings.TrimSpace(ts.Getenv("SOFT_SERVE_BACKUP_SECRET_KEY"))
	region := strings.TrimSpace(ts.Getenv("SOFT_SERVE_BACKUP_REGION"))
	bucket := strings.TrimSpace(ts.Getenv("SOFT_SERVE_BACKUP_BUCKET"))
	if endpoint == "" || access == "" || secret == "" || bucket == "" {
		ts.Fatalf("SOFT_SERVE_BACKUP_{ENDPOINT,ACCESS_KEY,SECRET_KEY,BUCKET} must be set")
	}
	if region == "" {
		region = "garage"
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(access, secret, ""),
		Region: region,
		Secure: false,
	})
	if err != nil {
		ts.Fatalf("minio.New: %v", err)
	}
	return client, bucket, region
}

func stripScheme(endpoint string) string {
	if !strings.Contains(endpoint, "://") {
		return endpoint
	}
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return endpoint
}
