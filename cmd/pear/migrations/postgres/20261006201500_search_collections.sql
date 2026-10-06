-- +goose Up
-- create "search_collections" table
CREATE TABLE "search_collections" (
  "org_did" text NOT NULL,
  "collection" text NOT NULL,
  "created_at" timestamptz NULL,
  PRIMARY KEY ("org_did", "collection")
);

-- +goose Down
-- reverse: create "search_collections" table
DROP TABLE "search_collections";
