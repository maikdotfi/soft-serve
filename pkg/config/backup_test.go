package config

import (
	"strings"
	"testing"

	"github.com/matryer/is"
)

// The hook subprocess receives its config through Environ(), so every
// backup setting must survive the Environ -> ParseEnv round trip.
func TestEnviron_BackupConfigRoundTripsThroughEnv(t *testing.T) {
	is := is.New(t)
	want := S3BackupConfig{
		Enabled:            true,
		S3Endpoint:         "https://s3.example.com",
		S3Bucket:           "bucket",
		S3Region:           "eu-north-1",
		S3PathPrefix:       "prefix",
		S3AccessKey:        "access",
		S3SecretKey:        "secret",
		ScheduleInterval:   "2h",
		MaxRepoBackups:     7,
		MaxServerSnapshots: 9,
		MaxUploadRetries:   4,
		UploadTimeout:      "15m",
	}
	src := DefaultConfig()
	src.Backup = want

	for _, kv := range src.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "SOFT_SERVE_BACKUP_") {
			t.Setenv(k, v)
		}
	}

	got := DefaultConfig()
	is.NoErr(got.ParseEnv())
	is.Equal(got.Backup, want)
}

func TestValidate_ReadsBackupCredentialsFromEnv(t *testing.T) {
	is := is.New(t)
	t.Setenv("SOFT_SERVE_BACKUP_ACCESS_KEY", "access")
	t.Setenv("SOFT_SERVE_BACKUP_SECRET_KEY", "secret")

	cfg := DefaultConfig()
	cfg.DataPath = t.TempDir()
	is.NoErr(cfg.Validate())
	is.Equal(cfg.Backup.S3AccessKey, "access")
	is.Equal(cfg.Backup.S3SecretKey, "secret")
}

func TestConvertBackupConfig_ParsesDurations(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		wantErr  bool
	}{
		{name: "go duration", interval: "90m"},
		{name: "integer seconds", interval: "3600"},
		{name: "garbage", interval: "soon", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultS3BackupConfig()
			c.ScheduleInterval = tt.interval
			_, err := ConvertBackupConfig(c)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertBackupConfig(interval=%q) err = %v, wantErr %v", tt.interval, err, tt.wantErr)
			}
		})
	}
}
