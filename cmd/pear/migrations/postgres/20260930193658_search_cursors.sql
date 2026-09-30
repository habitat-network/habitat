-- +goose Up
-- create "search_cursors" table
CREATE TABLE "search_cursors" (
  "space" text NOT NULL,
  "repo" text NOT NULL,
  "rev" text NULL,
  PRIMARY KEY ("space", "repo")
);

-- +goose Down
-- reverse: create "search_cursors" table
DROP TABLE "search_cursors";
