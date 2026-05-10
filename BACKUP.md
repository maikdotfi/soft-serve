# S3 Backup and Restore

Soft Serve can back up your repositories and server data to any
S3-compatible object store and restore them on demand. This is useful for
disaster recovery, migrating to a new host, or simply keeping an off-site copy
of everything.

## How it works

Backups are **schedule-driven**: every time the backup schedule fires,
Soft Serve creates two artifacts:

- **Repo backups** – a `git bundle` of every repository, uploaded to S3.
- **Server snapshots** – a full archive of the server database, config,
  and SSH keys.

Both are produced on the same tick. The recovery point objective (RPO) is
bounded by `schedule_interval` — a push that lands between two ticks is
not in S3 until the next one fires. Tune the interval to match your
tolerance for data loss.

Backups are rotated automatically. When the configured limit is exceeded
the oldest stored backups are pruned from both S3 _and_ the database.

Restoring is done with the `soft restore` subcommand. It downloads the
latest server snapshot and the latest stored bundle for every repository,
and restores them in order.

## Enabling S3 backup

Backup is **disabled by default**. You enable it in `config.yaml` or with
environment variables.

### Via config.yaml

Add a `backup` block to your `config.yaml`:

```yaml
backup:
  # Enable S3 backup.
  enabled: true

  # S3-compatible endpoint URL.
  # For AWS use "https://s3.amazonaws.com".
  # For MinIO use "http://localhost:9000".
  # For Garage use your S3 API endpoint.
  endpoint: "https://s3.example.com"

  # S3 bucket name.
  bucket: "my-soft-serve-backups"

  # S3 region.
  region: "us-east-1"

  # Key prefix inside the bucket (default: "soft-serve").
  path_prefix: "soft-serve"

  # How often the backup schedule fires (default: "6h").
  # Accepts Go duration strings such as "30m", "2h", "24h",
  # or a plain integer interpreted as seconds.
  schedule_interval: "6h"

  # Maximum number of stored repo backups per repository (default: 5).
  max_repo_backups: 5

  # Maximum number of stored server snapshots (default: 30).
  max_server_snapshots: 30

  # Maximum number of upload retries before marking a backup as failed (default: 3).
  max_upload_retries: 3

  # Maximum time an upload may take before it is marked failed (default: "1h").
  upload_timeout: "1h"
```

### Via environment variables

Every field can also be set with the `SOFT_SERVE_BACKUP_` prefix:

| Config key           | Environment variable                            | Default       |
|----------------------|-------------------------------------------------|---------------|
| `enabled`            | `SOFT_SERVE_BACKUP_ENABLED`                     | `false`       |
| `endpoint`           | `SOFT_SERVE_BACKUP_ENDPOINT`                    | _(required)_  |
| `bucket`             | `SOFT_SERVE_BACKUP_BUCKET`                      | _(required)_  |
| `region`             | `SOFT_SERVE_BACKUP_REGION`                      | _(required)_  |
| `path_prefix`        | `SOFT_SERVE_BACKUP_PATH_PREFIX`                 | `soft-serve`  |
| `schedule_interval`  | `SOFT_SERVE_BACKUP_SCHEDULE_INTERVAL`           | `6h`          |
| `max_repo_backups`   | `SOFT_SERVE_BACKUP_MAX_REPO_BACKUPS`            | `5`           |
| `max_server_snapshots`| `SOFT_SERVE_BACKUP_MAX_SERVER_SNAPSHOTS`       | `30`          |
| `max_upload_retries` | `SOFT_SERVE_BACKUP_MAX_UPLOAD_RETRIES`          | `3`           |
| `upload_timeout`     | `SOFT_SERVE_BACKUP_UPLOAD_TIMEOUT`              | `1h`          |

> **Note** The `access_key` and `secret_key` fields are **never** written to
> `config.yaml`. They must be provided via environment variables (see below).

## S3 credentials

S3 credentials must be supplied through environment variables only — they are
never persisted in the config file:

```sh
export SOFT_SERVE_BACKUP_ACCESS_KEY="AKIAIOSFODNN7EXAMPLE"
export SOFT_SERVE_BACKUP_SECRET_KEY="wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
```

Make sure the IAM user or S3 account tied to these keys has permission to
create, read, and delete objects in the configured bucket.

## S3-compatible endpoints

Soft Serve works with any S3-compatible API. Common options include:

| Provider   | Example endpoint                        |
|------------|-----------------------------------------|
| AWS S3     | `https://s3.amazonaws.com`               |
| MinIO      | `http://localhost:9000`                 |
| Garage     | Your Garage S3 API endpoint             |
| Cloudflare R2 | `https://<account-id>.r2.cloudflarestorage.com` |

The bucket must already exist or the server must have permission to create it.
On first run Soft Serve will call `EnsureBucket` to verify the bucket is
accessible.

## Backup triggers

### On schedule

The backup schedule is the only automatic trigger. When it fires
(controlled by `schedule_interval`), Soft Serve creates one
**server snapshot** plus one **repo backup** per repository.

Choose `schedule_interval` to match your acceptable RPO: a push that
lands between ticks is not in S3 until the next tick.

### Manual

Admins can trigger a server snapshot or a repo backup on demand outside
the schedule via the `AdminBackupManagement` surface. This is useful for
ad-hoc snapshots before maintenance or migrations.

## Retention and rotation

- Each repository keeps at most `max_repo_backups` stored backups. When a new
  backup is stored, the oldest stored backups beyond the limit are deleted
  from both S3 and the database.
- The server keeps at most `max_server_snapshots` stored snapshots, using the
  same rotation logic.

## Upload reliability

- Failed uploads are retried up to `max_upload_retries` times with exponential
  backoff (capped at 5 minutes).
- If an upload exceeds `upload_timeout` it is automatically marked as failed.
- Periodically the server enforces timeouts for any uploads that have been
  stuck in the `uploading` state for too long.

## Restoring from a backup

Use the `soft restore` subcommand to restore the server from the latest S3
backup:

```sh
soft restore
```

This command:

1. Connects to the S3 bucket using the same backup configuration.
2. Downloads the most recent **stored** server snapshot and restores the
   database, config, and SSH keys.
3. Downloads the most recent **stored** bundle for each repository and
   restores it.
4. Reports the restore job ID for tracking.

> **Warning** Restore overwrites existing server data. Make sure you have a
> backup of the current data directory if you need it.

### Force flag

If there are already active or failed restore jobs, `soft restore` will refuse
to start a new one. Use `--force` to override this:

```sh
soft restore --force
```

### Prerequisites

Before running `soft restore`, make sure:

- S3 backup is enabled (`SOFT_SERVE_BACKUP_ENABLED=true`).
- The S3 endpoint, bucket, and region are configured.
- The server can reach the S3 bucket (network connectivity, correct
  credentials).
- The database is initialized (Soft Serve will create/initialize it if needed).

## Example: full setup with AWS S3

```sh
# Set admin key for initial setup
export SOFT_SERVE_INITIAL_ADMIN_KEYS="ssh-ed25519 AAAAC3NzaC1lZDI1..."

# Enable S3 backup
export SOFT_SERVE_BACKUP_ENABLED=true
export SOFT_SERVE_BACKUP_ENDPOINT="https://s3.amazonaws.com"
export SOFT_SERVE_BACKUP_BUCKET="my-soft-serve-backups"
export SOFT_SERVE_BACKUP_REGION="us-east-1"

# S3 credentials (never stored in config.yaml)
export SOFT_SERVE_BACKUP_ACCESS_KEY="AKIAIOSFODNN7EXAMPLE"
export SOFT_SERVE_BACKUP_SECRET_KEY="wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"

# Start the server
soft serve
```

To restore on a new host:

```sh
# Same environment variables as above
export SOFT_SERVE_BACKUP_ENABLED=true
export SOFT_SERVE_BACKUP_ENDPOINT="https://s3.amazonaws.com"
export SOFT_SERVE_BACKUP_BUCKET="my-soft-serve-backups"
export SOFT_SERVE_BACKUP_REGION="us-east-1"
export SOFT_SERVE_BACKUP_ACCESS_KEY="AKIAIOSFODNN7EXAMPLE"
export SOFT_SERVE_BACKUP_SECRET_KEY="wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"

soft restore
```

