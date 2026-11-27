-- +goose Up
-- create "join_request_rows" table
CREATE TABLE "join_request_rows" (
  "org_id" text NOT NULL,
  "member" text NOT NULL,
  "message" text NOT NULL,
  "created_at" timestamptz NOT NULL,
  PRIMARY KEY ("org_id", "member")
);

-- +goose Down
-- reverse: create "join_request_rows" table
DROP TABLE "join_request_rows";
