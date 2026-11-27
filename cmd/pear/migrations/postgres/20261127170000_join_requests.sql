-- +goose Up
-- create "join_requests" table
CREATE TABLE "join_requests" (
  "org_id" text NOT NULL,
  "member" text NOT NULL,
  "message" text NOT NULL,
  "created_at" timestamptz NOT NULL,
  PRIMARY KEY ("org_id", "member")
);

-- +goose Down
-- reverse: create "join_requests" table
DROP TABLE "join_requests";
