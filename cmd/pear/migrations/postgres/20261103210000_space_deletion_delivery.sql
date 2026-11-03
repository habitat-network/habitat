-- +goose Up
-- create "deleted_spaces" table
CREATE TABLE "deleted_spaces" (
  "owner" text NOT NULL,
  "type" text NOT NULL,
  "skey" text NOT NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("owner", "type", "skey")
);
-- create "space_deleted_deliveries" table
CREATE TABLE "space_deleted_deliveries" (
  "space" text NOT NULL,
  "audience" text NOT NULL,
  "endpoint" text NULL,
  "namespace" text NULL,
  "expires_at" timestamptz NULL,
  "attempts" bigint NULL,
  "retry_at" bigint NULL,
  PRIMARY KEY ("space", "audience")
);
-- create index "idx_space_deleted_deliveries_retry_at" to table: "space_deleted_deliveries"
CREATE INDEX "idx_space_deleted_deliveries_retry_at" ON "space_deleted_deliveries" ("retry_at");

-- +goose Down
-- reverse: create index "idx_space_deleted_deliveries_retry_at" to table: "space_deleted_deliveries"
DROP INDEX "idx_space_deleted_deliveries_retry_at";
-- reverse: create "space_deleted_deliveries" table
DROP TABLE "space_deleted_deliveries";
-- reverse: create "deleted_spaces" table
DROP TABLE "deleted_spaces";
