-- +goose Up
-- create "deleted_spaces" table
CREATE TABLE `deleted_spaces` (
  `owner` text NULL,
  `type` text NULL,
  `skey` text NULL,
  `deleted_at` datetime NULL,
  PRIMARY KEY (`owner`, `type`, `skey`)
);
-- create "space_deleted_deliveries" table
CREATE TABLE `space_deleted_deliveries` (
  `space` text NULL,
  `audience` text NULL,
  `endpoint` text NULL,
  `namespace` text NULL,
  `expires_at` datetime NULL,
  `attempts` integer NULL,
  `retry_at` integer NULL,
  PRIMARY KEY (`space`, `audience`)
);
-- create index "idx_space_deleted_deliveries_retry_at" to table: "space_deleted_deliveries"
CREATE INDEX `idx_space_deleted_deliveries_retry_at` ON `space_deleted_deliveries` (`retry_at`);

-- +goose Down
-- reverse: create index "idx_space_deleted_deliveries_retry_at" to table: "space_deleted_deliveries"
DROP INDEX `idx_space_deleted_deliveries_retry_at`;
-- reverse: create "space_deleted_deliveries" table
DROP TABLE `space_deleted_deliveries`;
-- reverse: create "deleted_spaces" table
DROP TABLE `deleted_spaces`;
