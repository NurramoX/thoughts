CREATE TABLE thought (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  title       TEXT    NOT NULL,
  body        TEXT    NOT NULL,
  version     INTEGER NOT NULL,
  created_at  TEXT    NOT NULL,
  updated_at  TEXT    NOT NULL,
  reviewed_at TEXT
);
CREATE INDEX thought_created_at ON thought(created_at);
CREATE INDEX thought_updated_at ON thought(updated_at);
CREATE INDEX thought_reviewed_at ON thought(reviewed_at);

CREATE TABLE thought_tag (
  thought_id INTEGER NOT NULL REFERENCES thought(id) ON DELETE CASCADE,
  tag     TEXT    NOT NULL,
  PRIMARY KEY (thought_id, tag)
) WITHOUT ROWID;
CREATE INDEX thought_tag_tag ON thought_tag(tag);

CREATE TABLE thought_attribute (
  thought_id      INTEGER NOT NULL REFERENCES thought(id) ON DELETE CASCADE,
  key          TEXT    NOT NULL,
  value        TEXT    NOT NULL,
  value_folded TEXT    NOT NULL,
  PRIMARY KEY (thought_id, key)
) WITHOUT ROWID;
CREATE INDEX thought_attribute_key_value ON thought_attribute(key, value_folded);

CREATE VIRTUAL TABLE thought_fts USING fts5(
  title, body,
  content='thought', content_rowid='id',
  tokenize='porter unicode61 remove_diacritics 2'
);
CREATE TRIGGER thought_ai AFTER INSERT ON thought BEGIN
  INSERT INTO thought_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;
CREATE TRIGGER thought_ad AFTER DELETE ON thought BEGIN
  INSERT INTO thought_fts(thought_fts, rowid, title, body) VALUES ('delete', old.id, old.title, old.body);
END;
CREATE TRIGGER thought_au AFTER UPDATE ON thought BEGIN
  INSERT INTO thought_fts(thought_fts, rowid, title, body) VALUES ('delete', old.id, old.title, old.body);
  INSERT INTO thought_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;
INSERT INTO thought_fts(thought_fts, rank) VALUES('rank', 'bm25(10.0, 1.0)');
