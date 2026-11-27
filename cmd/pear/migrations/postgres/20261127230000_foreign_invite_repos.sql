-- +goose Up
CREATE TABLE "foreign_invite_repos" (
  "space_owner" text NOT NULL,
  "space_type" text NOT NULL,
  "space_skey" text NOT NULL,
  "author" text NOT NULL,
  "rev" text NOT NULL,
  "state" bytea NOT NULL,
  PRIMARY KEY ("space_owner", "space_type", "space_skey", "author")
);
CREATE TABLE "foreign_invite_records" (
  "space_owner" text NOT NULL,
  "space_type" text NOT NULL,
  "space_skey" text NOT NULL,
  "author" text NOT NULL,
  "rkey" text NOT NULL,
  "cid" text NOT NULL,
  "value" bytea NOT NULL,
  PRIMARY KEY ("space_owner", "space_type", "space_skey", "author", "rkey")
);

-- +goose Down
DROP TABLE "foreign_invite_records";
DROP TABLE "foreign_invite_repos";
