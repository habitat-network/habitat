-- +goose Up
-- create "space_remote_records" table
CREATE TABLE `space_remote_records` (
  `space` text NULL,
  `repo` text NULL,
  `collection` text NULL,
  `rkey` text NULL,
  `value` blob NULL,
  `cid` text NULL,
  `prev_cid` text NULL,
  `rev` text NULL,
  `seq` integer NOT NULL DEFAULT 0,
  `created_at` datetime NULL,
  `updated_at` datetime NULL,
  `deleted_at` datetime NULL,
  PRIMARY KEY (`space`, `repo`, `collection`, `rkey`)
);
-- create index "idx_space_remote_records_seq" to table: "space_remote_records"
CREATE INDEX `idx_space_remote_records_seq` ON `space_remote_records` (`seq`);
-- create "space_remote_syncs" table
CREATE TABLE `space_remote_syncs` (
  `space` text NULL,
  `repo` text NULL,
  `rev` text NULL,
  `state` blob NULL,
  `updated_at` datetime NULL,
  PRIMARY KEY (`space`, `repo`)
);

-- +goose Down
-- reverse: create "space_remote_syncs" table
DROP TABLE `space_remote_syncs`;
-- reverse: create index "idx_space_remote_records_seq" to table: "space_remote_records"
DROP INDEX `idx_space_remote_records_seq`;
-- reverse: create "space_remote_records" table
DROP TABLE `space_remote_records`;
