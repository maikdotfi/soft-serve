CREATE TABLE IF NOT EXISTS work_items (
  id SERIAL PRIMARY KEY,
  repo_id INTEGER NOT NULL,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  lane TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  CHECK (lane IN ('backlog', 'wip', 'done')),
  CONSTRAINT repo_id_fk
  FOREIGN KEY(repo_id) REFERENCES repos(id)
  ON DELETE CASCADE
  ON UPDATE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_work_items_repo_lane_id ON work_items (repo_id, lane, id);
