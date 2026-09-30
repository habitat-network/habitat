-- The full-text index over search_documents.body. Atlas can't model an FTS5
-- table or triggers, so this migration is hand-written and atlas.hcl excludes
-- these objects from the drift check. go-sqlite3 needs the sqlite_fts5 build
-- tag to run it.
--
-- search_documents_fts is an external-content table: it stores only the tokens
-- and reads text back from search_documents by rowid, so the text isn't stored
-- twice. The triggers mirror every insert, update and delete into it.

-- +goose Up
CREATE VIRTUAL TABLE `search_documents_fts` USING fts5(
  body,
  content='search_documents',
  content_rowid='rowid',
  tokenize='unicode61 remove_diacritics 2'
);

-- +goose StatementBegin
CREATE TRIGGER `search_documents_ai` AFTER INSERT ON `search_documents` BEGIN
  INSERT INTO search_documents_fts(rowid, body) VALUES (new.rowid, new.body);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER `search_documents_ad` AFTER DELETE ON `search_documents` BEGIN
  INSERT INTO search_documents_fts(search_documents_fts, rowid, body)
  VALUES ('delete', old.rowid, old.body);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER `search_documents_au` AFTER UPDATE ON `search_documents` BEGIN
  INSERT INTO search_documents_fts(search_documents_fts, rowid, body)
  VALUES ('delete', old.rowid, old.body);
  INSERT INTO search_documents_fts(rowid, body) VALUES (new.rowid, new.body);
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER `search_documents_au`;
DROP TRIGGER `search_documents_ad`;
DROP TRIGGER `search_documents_ai`;
DROP TABLE `search_documents_fts`;
