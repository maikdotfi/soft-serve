CREATE TABLE IF NOT EXISTS backup_schedule (
  id SERIAL PRIMARY KEY,
  next_run_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS repo_backups (
  id SERIAL PRIMARY KEY,
  repo_name TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  retry_count INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'uploading',
  CHECK (status IN ('uploading', 'stored', 'failed'))
);

CREATE INDEX IF NOT EXISTS idx_repo_backups_repo_name ON repo_backups (repo_name);
CREATE INDEX IF NOT EXISTS idx_repo_backups_status ON repo_backups (status);
CREATE INDEX IF NOT EXISTS idx_repo_backups_created_at ON repo_backups (created_at);

CREATE TABLE IF NOT EXISTS server_snapshots (
  id SERIAL PRIMARY KEY,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  retry_count INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'uploading',
  CHECK (status IN ('uploading', 'stored', 'failed'))
);

CREATE INDEX IF NOT EXISTS idx_server_snapshots_status ON server_snapshots (status);
CREATE INDEX IF NOT EXISTS idx_server_snapshots_created_at ON server_snapshots (created_at);

CREATE TABLE IF NOT EXISTS restore_jobs (
  id SERIAL PRIMARY KEY,
  status TEXT NOT NULL DEFAULT 'starting',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK (status IN ('starting', 'restoring_server', 'restoring_repos', 'completed', 'failed'))
);