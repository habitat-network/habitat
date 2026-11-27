-- +goose Up
-- create "moderation_subject_rows" table
CREATE TABLE "moderation_subject_rows" (
  "org_id" text NOT NULL,
  "subject" text NOT NULL,
  "cid" text NOT NULL,
  "author" text NOT NULL,
  "status" text NOT NULL,
  "tags" text NOT NULL,
  "report_count" bigint NOT NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("org_id", "subject", "cid")
);
-- create index "idx_moderation_subject_status" to table: "moderation_subject_rows"
CREATE INDEX "idx_moderation_subject_status" ON "moderation_subject_rows" ("org_id", "status", "updated_at");
-- create "moderation_event_rows" table
CREATE TABLE "moderation_event_rows" (
  "id" text NOT NULL,
  "org_id" text NOT NULL,
  "subject" text NOT NULL,
  "cid" text NOT NULL,
  "author" text NOT NULL,
  "kind" text NOT NULL,
  "created_by" text NOT NULL,
  "payload" text NOT NULL,
  "created_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- create index "idx_moderation_event_author" to table: "moderation_event_rows"
CREATE INDEX "idx_moderation_event_author" ON "moderation_event_rows" ("org_id", "author");
-- create index "idx_moderation_event_subject" to table: "moderation_event_rows"
CREATE INDEX "idx_moderation_event_subject" ON "moderation_event_rows" ("org_id", "subject");

-- +goose Down
-- reverse: create index "idx_moderation_event_subject" to table: "moderation_event_rows"
DROP INDEX "idx_moderation_event_subject";
-- reverse: create index "idx_moderation_event_author" to table: "moderation_event_rows"
DROP INDEX "idx_moderation_event_author";
-- reverse: create "moderation_event_rows" table
DROP TABLE "moderation_event_rows";
-- reverse: create index "idx_moderation_subject_status" to table: "moderation_subject_rows"
DROP INDEX "idx_moderation_subject_status";
-- reverse: create "moderation_subject_rows" table
DROP TABLE "moderation_subject_rows";
