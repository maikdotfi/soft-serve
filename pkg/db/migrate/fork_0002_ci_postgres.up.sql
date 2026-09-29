CREATE TABLE IF NOT EXISTS ci_runner_registrations (
  name TEXT PRIMARY KEY,
  dispatch_url TEXT NOT NULL,
  secret_token TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ci_workflows (
  id SERIAL PRIMARY KEY,
  repo_name TEXT NOT NULL,
  name TEXT NOT NULL,
  script TEXT NOT NULL,
  runs_on TEXT NOT NULL,
  container TEXT,
  triggers TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (repo_name, name)
);

CREATE INDEX IF NOT EXISTS idx_ci_workflows_repo_name ON ci_workflows (repo_name);

CREATE TABLE IF NOT EXISTS ci_runs (
  id SERIAL PRIMARY KEY,
  repo_name TEXT NOT NULL,
  workflow_name TEXT NOT NULL,
  script TEXT NOT NULL,
  runs_on TEXT NOT NULL,
  container TEXT,
  triggered_by_event TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL,
  started_at TIMESTAMP,
  finished_at TIMESTAMP,
  failure_reason TEXT,
  CHECK (status IN ('pending', 'dispatched', 'running', 'succeeded', 'failed', 'canceled'))
);

CREATE INDEX IF NOT EXISTS idx_ci_runs_repo_name ON ci_runs (repo_name);
CREATE INDEX IF NOT EXISTS idx_ci_runs_status ON ci_runs (status);
CREATE INDEX IF NOT EXISTS idx_ci_runs_runs_on ON ci_runs (runs_on);
CREATE INDEX IF NOT EXISTS idx_ci_runs_finished_at ON ci_runs (finished_at);

CREATE TABLE IF NOT EXISTS ci_log_entries (
  id SERIAL PRIMARY KEY,
  run_id BIGINT NOT NULL REFERENCES ci_runs (id) ON DELETE CASCADE,
  line TEXT NOT NULL,
  received_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ci_log_entries_run_id ON ci_log_entries (run_id);
