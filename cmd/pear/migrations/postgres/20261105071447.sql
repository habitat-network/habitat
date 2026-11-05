-- +goose Up
-- drop "instance_settings" table
DROP TABLE "instance_settings";

-- +goose Down
-- reverse: drop "instance_settings" table
CREATE TABLE "instance_settings" (
  "id" bigserial NOT NULL,
  "instance_name" text NULL,
  "org_creation_policy" text NULL,
  "signing_secret" text NULL,
  "created_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
