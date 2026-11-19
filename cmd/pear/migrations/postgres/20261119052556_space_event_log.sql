-- +goose Up
-- modify "deleted_spaces" table
ALTER TABLE "deleted_spaces" ADD COLUMN "seq" bigint NOT NULL DEFAULT 0;
-- create index "idx_deleted_spaces_seq" to table: "deleted_spaces"
CREATE INDEX "idx_deleted_spaces_seq" ON "deleted_spaces" ("seq");
-- modify "space_records" table
ALTER TABLE "space_records" ADD COLUMN "seq" bigint NOT NULL DEFAULT 0;
-- create index "idx_space_records_seq" to table: "space_records"
CREATE INDEX "idx_space_records_seq" ON "space_records" ("seq");
-- create "space_event_seqs" table
CREATE TABLE "space_event_seqs" (
  "id" bigint NOT NULL,
  "seq" bigint NULL,
  PRIMARY KEY ("id")
);

-- +goose Down
-- reverse: create "space_event_seqs" table
DROP TABLE "space_event_seqs";
-- reverse: create index "idx_space_records_seq" to table: "space_records"
DROP INDEX "idx_space_records_seq";
-- reverse: modify "space_records" table
ALTER TABLE "space_records" DROP COLUMN "seq";
-- reverse: create index "idx_deleted_spaces_seq" to table: "deleted_spaces"
DROP INDEX "idx_deleted_spaces_seq";
-- reverse: modify "deleted_spaces" table
ALTER TABLE "deleted_spaces" DROP COLUMN "seq";
