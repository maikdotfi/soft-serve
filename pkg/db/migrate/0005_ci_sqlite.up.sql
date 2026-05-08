CREATE TABLE IF NOT EXISTS ci_runner_registrations (
  name TEXT PRIMARY KEY,
  dispatch_url TEXT NOT NULL,
  secret_token TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ci_workflows (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  repo_name TEXT NOT NULL,
  name TEXT NOT NULL,
  script TEXT NOT NULL,
  runs_on TEXT NOT NULL,
  container TEXT,
  triggers TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (repo_name, name)
);

CREATE INDEX IF NOT EXISTS idx_ci_workflows_repo_name ON ci_workflows (repo_name);

CREATE TABLE IF NOT EXISTS ci_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  repo_name TEXT NOT NULL,
  workflow_name TEXT NOT NULL,
  script TEXT NOT NULL,
  runs_on TEXT NOT NULL,
  container TEXT,
  triggered_by_event TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  started_at DATETIME,
  finished_at DATETIME,
  failure_reason TEXT,
  CHECK (status IN ('pending', 'dispatched', 'running', 'succeeded', 'failed', 'canceled'))
);

CREATE INDEX IF NOT EXISTS idx_ci_runs_repo_name ON ci_runs (repo_name);
CREATE INDEX IF NOT EXISTS idx_ci_runs_status ON ci_runs (status);
CREATE INDEX IF NOT EXISTS idx_ci_runs_runs_on ON ci_runs (runs_on);
CREATE INDEX IF NOT EXISTS idx_ci_runs_finished_at ON ci_runs (finished_at);

CREATE TABLE IF NOT EXISTS ci_log_entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id INTEGER NOT NULL REFERENCES ci_runs (id) ON DELETE CASCADE,
  line TEXT NOT NULL,
  received_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ci_log_entries_run_id ON ci_log_entries (run_id);
