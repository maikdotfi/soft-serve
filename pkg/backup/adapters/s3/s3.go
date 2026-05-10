// Package s3 provides an adapter for S3-compatible object storage.
// Per AGENTS.md: this is an adapter that implements the backup.S3Provider port.
// The domain package never imports this package.
package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config holds configuration for connecting to an S3-compatible service.
type S3Config struct {
	Endpoint   string
	Region     string
	Bucket     string
	PathPrefix string
	AccessKey  string
	SecretKey  string
	Secure     bool // Use HTTPS
}

// Adapter implements backup.S3Provider using the MinIO SDK.
type Adapter struct {
	client *minio.Client
	config S3Config
}

// NewAdapter creates a new S3 adapter with the given configuration.
func NewAdapter(cfg S3Config) (*Adapter, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.Region == "" {
		return nil, fmt.Errorf("S3 endpoint, bucket, and region are required")
	}

	endpoint, secure := normalizeEndpoint(cfg.Endpoint, cfg.Secure)

	client, err := minio.New(endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Region:    cfg.Region,
		Secure:    secure,
		Transport: http.DefaultTransport,
	})
	if err != nil {
		return nil, fmt.Errorf("creating S3 client: %w", err)
	}

	return &Adapter{
		client: client,
		config: cfg,
	}, nil
}

// normalizeEndpoint converts a user-supplied endpoint into the bare
// host[:port] form the MinIO SDK requires. An "http://" or "https://" prefix
// implies the Secure setting; otherwise non-localhost endpoints default to
// secure to match prior behavior.
func normalizeEndpoint(raw string, secureFlag bool) (string, bool) {
	endpoint := raw
	secure := secureFlag
	schemeFound := false
	switch {
	case strings.HasPrefix(endpoint, "https://"):
		endpoint = strings.TrimPrefix(endpoint, "https://")
		secure = true
		schemeFound = true
	case strings.HasPrefix(endpoint, "http://"):
		endpoint = strings.TrimPrefix(endpoint, "http://")
		secure = false
		schemeFound = true
	}
	if i := strings.Index(endpoint, "/"); i >= 0 {
		endpoint = endpoint[:i]
	}

	if !schemeFound && !secureFlag {
		host := endpoint
		if i := strings.LastIndex(endpoint, ":"); i >= 0 {
			host = endpoint[:i]
		}
		if host != "localhost" && host != "127.0.0.1" {
			secure = true
		}
	}
	return endpoint, secure
}

// objectKey constructs the S3 object key for a repo backup.
func (a *Adapter) repoBackupKey(repoName string, backupID int64) string {
	return path.Join(a.config.PathPrefix, "repos", repoName, fmt.Sprintf("%d.bundle", backupID))
}

// objectKey constructs the S3 object key for a server snapshot.
func (a *Adapter) serverSnapshotKey(snapshotID int64) string {
	return path.Join(a.config.PathPrefix, "server", fmt.Sprintf("%d.tar", snapshotID))
}

// UploadRepoBackup uploads a git bundle for the given repo backup.
func (a *Adapter) UploadRepoBackup(ctx context.Context, repoName string, backupID int64, content []byte) error {
	key := a.repoBackupKey(repoName, backupID)
	reader := bytes.NewReader(content)
	_, err := a.client.PutObject(ctx, a.config.Bucket, key, reader, int64(len(content)), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("uploading repo backup to S3: %w", err)
	}
	return nil
}

// DownloadRepoBackup downloads a git bundle for the given repo backup.
func (a *Adapter) DownloadRepoBackup(ctx context.Context, repoName string, backupID int64) ([]byte, error) {
	key := a.repoBackupKey(repoName, backupID)
	obj, err := a.client.GetObject(ctx, a.config.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("downloading repo backup from S3: %w", err)
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("reading repo backup from S3: %w", err)
	}
	return data, nil
}

// DeleteRepoBackup deletes the stored git bundle for the given repo backup.
func (a *Adapter) DeleteRepoBackup(ctx context.Context, repoName string, backupID int64) error {
	key := a.repoBackupKey(repoName, backupID)
	err := a.client.RemoveObject(ctx, a.config.Bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("deleting repo backup from S3: %w", err)
	}
	return nil
}

// UploadServerSnapshot uploads a server snapshot archive.
func (a *Adapter) UploadServerSnapshot(ctx context.Context, snapshotID int64, content []byte) error {
	key := a.serverSnapshotKey(snapshotID)
	reader := bytes.NewReader(content)
	_, err := a.client.PutObject(ctx, a.config.Bucket, key, reader, int64(len(content)), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("uploading server snapshot to S3: %w", err)
	}
	return nil
}

// DownloadServerSnapshot downloads a server snapshot archive.
func (a *Adapter) DownloadServerSnapshot(ctx context.Context, snapshotID int64) ([]byte, error) {
	key := a.serverSnapshotKey(snapshotID)
	obj, err := a.client.GetObject(ctx, a.config.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("downloading server snapshot from S3: %w", err)
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("reading server snapshot from S3: %w", err)
	}
	return data, nil
}

// DeleteServerSnapshot deletes the stored server snapshot archive.
func (a *Adapter) DeleteServerSnapshot(ctx context.Context, snapshotID int64) error {
	key := a.serverSnapshotKey(snapshotID)
	err := a.client.RemoveObject(ctx, a.config.Bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("deleting server snapshot from S3: %w", err)
	}
	return nil
}

// EnsureBucket creates the bucket if it doesn't exist.
func (a *Adapter) EnsureBucket(ctx context.Context) error {
	exists, err := a.client.BucketExists(ctx, a.config.Bucket)
	if err != nil {
		return fmt.Errorf("checking S3 bucket: %w", err)
	}
	if !exists {
		if err := a.client.MakeBucket(ctx, a.config.Bucket, minio.MakeBucketOptions{
			Region: a.config.Region,
		}); err != nil {
			return fmt.Errorf("creating S3 bucket: %w", err)
		}
	}
	return nil
}

// LastModified returns the last modified time for an object, used in testing.
func (a *Adapter) LastModified(ctx context.Context, key string) (time.Time, error) {
	info, err := a.client.StatObject(ctx, a.config.Bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return time.Time{}, err
	}
	return info.LastModified, nil
}