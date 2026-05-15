CREATE TABLE IF NOT EXISTS work_item_messages (
  id SERIAL PRIMARY KEY,
  work_item_id INTEGER NOT NULL,
  kind TEXT NOT NULL,
  body TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  CHECK (kind IN ('comment')),
  CONSTRAINT work_item_id_fk
  FOREIGN KEY(work_item_id) REFERENCES work_items(id)
  ON DELETE CASCADE
  ON UPDATE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_work_item_messages_work_item_id_id ON work_item_messages (work_item_id, id);
