-- +goose Up
-- create "search_documents" table
CREATE TABLE `search_documents` (
  `uri` text NULL,
  `space` text NULL,
  `repo` text NULL,
  `collection` text NULL,
  `rev` text NULL,
  `body` text NULL,
  PRIMARY KEY (`uri`)
);
-- create index "idx_search_documents_space" to table: "search_documents"
CREATE INDEX `idx_search_documents_space` ON `search_documents` (`space`);

-- +goose Down
-- reverse: create index "idx_search_documents_space" to table: "search_documents"
DROP INDEX `idx_search_documents_space`;
-- reverse: create "search_documents" table
DROP TABLE `search_documents`;
