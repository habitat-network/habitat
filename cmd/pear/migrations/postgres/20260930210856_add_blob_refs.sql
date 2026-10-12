-- +goose Up
-- create "blob_refs" table
CREATE TABLE "blob_refs" (
  "cid" text NOT NULL,
  "space" text NOT NULL,
  "repo" text NOT NULL,
  "collection" text NOT NULL,
  "rkey" text NOT NULL,
  PRIMARY KEY ("cid", "space", "repo", "collection", "rkey")
);

-- +goose Down
-- reverse: create "blob_refs" table
DROP TABLE "blob_refs";
