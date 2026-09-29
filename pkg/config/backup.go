package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// S3BackupConfig is the configuration for S3 backup and restore.
// Corresponds to the config block in backup.allium.
type S3BackupConfig struct {
	// Enabled controls whether S3 backup is active.
	Enabled bool `env:"ENABLED" yaml:"enabled"`

	// S3Endpoint is the S3-compatible endpoint URL.
	S3Endpoint string `env:"ENDPOINT" yaml:"endpoint"`

	// S3Bucket is the S3 bucket name.
	S3Bucket string `env:"BUCKET" yaml:"bucket"`

	// S3Region is the S3 region.
	S3Region string `env:"REGION" yaml:"region"`

	// S3PathPrefix is the key prefix within the bucket.
	S3PathPrefix string `env:"PATH_PREFIX" yaml:"path_prefix"`

	// S3AccessKey is the access key ID.
	S3AccessKey string `env:"ACCESS_KEY" yaml:"-"`

	// S3SecretKey is the secret access key.
	S3SecretKey string `env:"SECRET_KEY" yaml:"-"`

	// ScheduleInterval is how often the backup schedule fires.
	ScheduleInterval string `env:"SCHEDULE_INTERVAL" yaml:"schedule_interval"`

	// MaxRepoBackups is the maximum number of stored backups per repo.
	MaxRepoBackups int `env:"MAX_REPO_BACKUPS" yaml:"max_repo_backups"`

	// MaxServerSnapshots is the maximum number of stored server snapshots.
	MaxServerSnapshots int `env:"MAX_SERVER_SNAPSHOTS" yaml:"max_server_snapshots"`

	// MaxUploadRetries is the maximum number of upload retries.
	MaxUploadRetries int `env:"MAX_UPLOAD_RETRIES" yaml:"max_upload_retries"`

	// UploadTimeout is the maximum time an upload can take before being marked
	// failed. Parsed as a Go duration (e.g. "1h", "30m").
	UploadTimeout string `env:"UPLOAD_TIMEOUT" yaml:"upload_timeout"`
}

// DefaultS3BackupConfig returns the default S3 backup configuration.
func DefaultS3BackupConfig() S3BackupConfig {
	return S3BackupConfig{
		Enabled:            false,
		S3PathPrefix:       "soft-serve",
		ScheduleInterval:   "6h",
		MaxRepoBackups:     5,
		MaxServerSnapshots: 30,
		MaxUploadRetries:   3,
		UploadTimeout:      "1h",
	}
}

// Duration parses the ScheduleInterval as a time.Duration.
func (c S3BackupConfig) ScheduleIntervalDuration() (time.Duration, error) {
	return parseDuration(c.ScheduleInterval)
}

// UploadTimeoutDuration parses the UploadTimeout as a time.Duration.
func (c S3BackupConfig) UploadTimeoutDuration() (time.Duration, error) {
	return parseDuration(c.UploadTimeout)
}

// parseDuration parses a duration string that may use Go duration syntax
// (e.g. "1h", "30m") or a simple integer-in-seconds string.
func parseDuration(s string) (time.Duration, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	// Try as seconds
	if secs, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Duration(secs) * time.Second, nil
	}
	return 0, fmt.Errorf("invalid duration: %q", s)
}

// ConvertBackupConfig converts an S3BackupConfig from the server config
// into the domain backup.BackupConfig. This lives in the config package
// to avoid import cycles — the domain backup package must not import config.
func ConvertBackupConfig(c S3BackupConfig) (BackupConfigResult, error) {
	scheduleInterval, err := c.ScheduleIntervalDuration()
	if err != nil {
		return BackupConfigResult{}, fmt.Errorf("parsing backup schedule_interval: %w", err)
	}
	uploadTimeout, err := c.UploadTimeoutDuration()
	if err != nil {
		return BackupConfigResult{}, fmt.Errorf("parsing backup upload_timeout: %w", err)
	}
	return BackupConfigResult{
		S3Endpoint:         c.S3Endpoint,
		S3Bucket:           c.S3Bucket,
		S3Region:           c.S3Region,
		S3PathPrefix:       c.S3PathPrefix,
		ScheduleInterval:   scheduleInterval,
		MaxRepoBackups:     c.MaxRepoBackups,
		MaxServerSnapshots: c.MaxServerSnapshots,
		MaxUploadRetries:   c.MaxUploadRetries,
		UploadTimeout:      uploadTimeout,
	}, nil
}

// BackupConfigResult holds the parsed backup configuration values.
// This avoids the need to import the domain backup package.
type BackupConfigResult struct {
	S3Endpoint         string
	S3Bucket           string
	S3Region           string
	S3PathPrefix       string
	ScheduleInterval   time.Duration
	MaxRepoBackups     int
	MaxServerSnapshots int
	MaxUploadRetries   int
	UploadTimeout      time.Duration
}

// environ returns the SOFT_SERVE_BACKUP_* variables that Config.Environ
// passes to hook subprocesses.
func (c S3BackupConfig) environ() []string {
	return []string{
		fmt.Sprintf("SOFT_SERVE_BACKUP_ENABLED=%t", c.Enabled),
		fmt.Sprintf("SOFT_SERVE_BACKUP_ENDPOINT=%s", c.S3Endpoint),
		fmt.Sprintf("SOFT_SERVE_BACKUP_BUCKET=%s", c.S3Bucket),
		fmt.Sprintf("SOFT_SERVE_BACKUP_REGION=%s", c.S3Region),
		fmt.Sprintf("SOFT_SERVE_BACKUP_PATH_PREFIX=%s", c.S3PathPrefix),
		fmt.Sprintf("SOFT_SERVE_BACKUP_ACCESS_KEY=%s", c.S3AccessKey),
		fmt.Sprintf("SOFT_SERVE_BACKUP_SECRET_KEY=%s", c.S3SecretKey),
		fmt.Sprintf("SOFT_SERVE_BACKUP_SCHEDULE_INTERVAL=%s", c.ScheduleInterval),
		fmt.Sprintf("SOFT_SERVE_BACKUP_MAX_REPO_BACKUPS=%d", c.MaxRepoBackups),
		fmt.Sprintf("SOFT_SERVE_BACKUP_MAX_SERVER_SNAPSHOTS=%d", c.MaxServerSnapshots),
		fmt.Sprintf("SOFT_SERVE_BACKUP_MAX_UPLOAD_RETRIES=%d", c.MaxUploadRetries),
		fmt.Sprintf("SOFT_SERVE_BACKUP_UPLOAD_TIMEOUT=%s", c.UploadTimeout),
	}
}

// loadCredentialsFromEnv fills the S3 credentials from the environment. They
// are never read from or written to the config file.
func (c *S3BackupConfig) loadCredentialsFromEnv() {
	if c.S3AccessKey == "" {
		c.S3AccessKey = os.Getenv("SOFT_SERVE_BACKUP_ACCESS_KEY")
	}
	if c.S3SecretKey == "" {
		c.S3SecretKey = os.Getenv("SOFT_SERVE_BACKUP_SECRET_KEY")
	}
}
