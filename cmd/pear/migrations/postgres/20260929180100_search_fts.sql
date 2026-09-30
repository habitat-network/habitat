-- The full-text index over search_documents.body: a GIN expression index.
-- Atlas can't model it on the GORM model, so this migration is hand-written
-- and atlas.hcl excludes the index from the drift check. Queries must use the
-- same to_tsvector expression to hit it. The 'simple' text search config
-- doesn't stem or drop stop words, since orgs mix languages.

-- +goose Up
CREATE INDEX "search_documents_body_fts" ON "search_documents" USING GIN (to_tsvector('simple', "body"));

-- +goose Down
DROP INDEX "search_documents_body_fts";
