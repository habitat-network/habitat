-- +goose Up
-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_space_records" table
CREATE TABLE `new_space_records` (
  `space` text NULL,
  `space_owner` text NULL,
  `space_type` text NULL,
  `space_skey` text NULL,
  `repo` text NULL,
  `collection` text NULL,
  `rkey` text NULL,
  `value` blob NULL,
  `value_json` json NULL,
  `rev` text NULL,
  `cid` text NULL,
  `prev_cid` text NULL,
  `seq` integer NOT NULL DEFAULT 0,
  `created_at` datetime NULL,
  `updated_at` datetime NULL,
  `deleted_at` datetime NULL,
  PRIMARY KEY (`space_owner`, `space_type`, `space_skey`, `repo`, `collection`, `rkey`)
);
-- copy rows from old table "space_records" to new temporary table "new_space_records"
INSERT INTO `new_space_records` (`space`, `space_owner`, `space_type`, `space_skey`, `repo`, `collection`, `rkey`, `value`, `value_json`, `rev`, `cid`, `prev_cid`, `seq`, `created_at`, `updated_at`, `deleted_at`) SELECT `space`, `space_owner`, `space_type`, `space_skey`, `repo`, `collection`, `rkey`, `value`, `value_json`, `rev`, `cid`, `prev_cid`, `seq`, `created_at`, `updated_at`, `deleted_at` FROM `space_records`;
-- drop "space_records" table after copying rows
DROP TABLE `space_records`;
-- rename temporary table "new_space_records" to "space_records"
ALTER TABLE `new_space_records` RENAME TO `space_records`;
-- create index "idx_space_records_seq" to table: "space_records"
CREATE INDEX `idx_space_records_seq` ON `space_records` (`seq`);
-- create index "idx_space_records_rev" to table: "space_records"
CREATE UNIQUE INDEX `idx_space_records_rev` ON `space_records` (`rev`);
-- create "new_space_repos" table
CREATE TABLE `new_space_repos` (
  `space` text NULL,
  `space_owner` text NULL,
  `space_type` text NULL,
  `space_skey` text NULL,
  `repo` text NULL,
  `hash` blob NULL,
  `rev` text NULL,
  `space_rev` text NULL,
  `remote` numeric NULL,
  `removed` numeric NULL,
  `updated_at` datetime NULL,
  `deleted_at` datetime NULL,
  PRIMARY KEY (`space_owner`, `space_type`, `space_skey`, `repo`)
);
-- copy rows from old table "space_repos" to new temporary table "new_space_repos"
INSERT INTO `new_space_repos` (`space`, `space_owner`, `space_type`, `space_skey`, `repo`, `hash`, `rev`, `space_rev`, `remote`, `removed`, `updated_at`, `deleted_at`) SELECT `space`, `space_owner`, `space_type`, `space_skey`, `repo`, `hash`, `rev`, `space_rev`, `remote`, `removed`, `updated_at`, `deleted_at` FROM `space_repos`;
-- drop "space_repos" table after copying rows
DROP TABLE `space_repos`;
-- rename temporary table "new_space_repos" to "space_repos"
ALTER TABLE `new_space_repos` RENAME TO `space_repos`;
-- create "new_registrations" table
CREATE TABLE `new_registrations` (
  `space` text NULL,
  `space_owner` text NULL,
  `space_type` text NULL,
  `space_skey` text NULL,
  `repo` text NULL,
  `audience` text NULL,
  `endpoint` text NULL,
  `namespace` text NULL DEFAULT 'network.habitat.space',
  `expires_at` datetime NULL,
  `created_at` datetime NULL,
  `updated_at` datetime NULL,
  PRIMARY KEY (`space_owner`, `space_type`, `space_skey`, `repo`, `audience`)
);
-- copy rows from old table "registrations" to new temporary table "new_registrations"
INSERT INTO `new_registrations` (`space`, `space_owner`, `space_type`, `space_skey`, `repo`, `audience`, `endpoint`, `namespace`, `expires_at`, `created_at`, `updated_at`) SELECT `space`, `space_owner`, `space_type`, `space_skey`, `repo`, `audience`, `endpoint`, `namespace`, `expires_at`, `created_at`, `updated_at` FROM `registrations`;
-- drop "registrations" table after copying rows
DROP TABLE `registrations`;
-- rename temporary table "new_registrations" to "registrations"
ALTER TABLE `new_registrations` RENAME TO `registrations`;
-- create "new_blob_refs" table
CREATE TABLE `new_blob_refs` (
  `cid` text NULL,
  `space` text NULL,
  `space_owner` text NULL,
  `space_type` text NULL,
  `space_skey` text NULL,
  `repo` text NULL,
  `collection` text NULL,
  `rkey` text NULL,
  PRIMARY KEY (`cid`, `space_owner`, `space_type`, `space_skey`, `repo`, `collection`, `rkey`)
);
-- copy rows from old table "blob_refs" to new temporary table "new_blob_refs"
INSERT INTO `new_blob_refs` (`cid`, `space`, `space_owner`, `space_type`, `space_skey`, `repo`, `collection`, `rkey`) SELECT `cid`, `space`, `space_owner`, `space_type`, `space_skey`, `repo`, `collection`, `rkey` FROM `blob_refs`;
-- drop "blob_refs" table after copying rows
DROP TABLE `blob_refs`;
-- rename temporary table "new_blob_refs" to "blob_refs"
ALTER TABLE `new_blob_refs` RENAME TO `blob_refs`;
-- create "new_space_deleted_deliveries" table
CREATE TABLE `new_space_deleted_deliveries` (
  `space` text NULL,
  `space_owner` text NULL,
  `space_type` text NULL,
  `space_skey` text NULL,
  `audience` text NULL,
  `endpoint` text NULL,
  `namespace` text NULL,
  `expires_at` datetime NULL,
  `attempts` integer NULL,
  `retry_at` integer NULL,
  PRIMARY KEY (`space_owner`, `space_type`, `space_skey`, `audience`)
);
-- copy rows from old table "space_deleted_deliveries" to new temporary table "new_space_deleted_deliveries"
INSERT INTO `new_space_deleted_deliveries` (`space`, `space_owner`, `space_type`, `space_skey`, `audience`, `endpoint`, `namespace`, `expires_at`, `attempts`, `retry_at`) SELECT `space`, `space_owner`, `space_type`, `space_skey`, `audience`, `endpoint`, `namespace`, `expires_at`, `attempts`, `retry_at` FROM `space_deleted_deliveries`;
-- drop "space_deleted_deliveries" table after copying rows
DROP TABLE `space_deleted_deliveries`;
-- rename temporary table "new_space_deleted_deliveries" to "space_deleted_deliveries"
ALTER TABLE `new_space_deleted_deliveries` RENAME TO `space_deleted_deliveries`;
-- create index "idx_space_deleted_deliveries_retry_at" to table: "space_deleted_deliveries"
CREATE INDEX `idx_space_deleted_deliveries_retry_at` ON `space_deleted_deliveries` (`retry_at`);
-- create "new_space_webhooks" table
CREATE TABLE `new_space_webhooks` (
  `space` text NULL,
  `space_owner` text NULL,
  `space_type` text NULL,
  `space_skey` text NULL,
  `endpoint` text NULL,
  `token` text NULL,
  `expires_at` datetime NULL,
  `expiry_notified` numeric NULL,
  `created_at` datetime NULL,
  `updated_at` datetime NULL,
  PRIMARY KEY (`space_owner`, `space_type`, `space_skey`, `endpoint`, `token`)
);
-- copy rows from old table "space_webhooks" to new temporary table "new_space_webhooks"
INSERT INTO `new_space_webhooks` (`space`, `space_owner`, `space_type`, `space_skey`, `endpoint`, `token`, `expires_at`, `expiry_notified`, `created_at`, `updated_at`) SELECT `space`, `space_owner`, `space_type`, `space_skey`, `endpoint`, `token`, `expires_at`, `expiry_notified`, `created_at`, `updated_at` FROM `space_webhooks`;
-- drop "space_webhooks" table after copying rows
DROP TABLE `space_webhooks`;
-- rename temporary table "new_space_webhooks" to "space_webhooks"
ALTER TABLE `new_space_webhooks` RENAME TO `space_webhooks`;
-- create "new_space_remote_records" table
CREATE TABLE `new_space_remote_records` (
  `space` text NULL,
  `space_owner` text NULL,
  `space_type` text NULL,
  `space_skey` text NULL,
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
  PRIMARY KEY (`space_owner`, `space_type`, `space_skey`, `repo`, `collection`, `rkey`)
);
-- copy rows from old table "space_remote_records" to new temporary table "new_space_remote_records"
INSERT INTO `new_space_remote_records` (`space`, `space_owner`, `space_type`, `space_skey`, `repo`, `collection`, `rkey`, `value`, `cid`, `prev_cid`, `rev`, `seq`, `created_at`, `updated_at`, `deleted_at`) SELECT `space`, `space_owner`, `space_type`, `space_skey`, `repo`, `collection`, `rkey`, `value`, `cid`, `prev_cid`, `rev`, `seq`, `created_at`, `updated_at`, `deleted_at` FROM `space_remote_records`;
-- drop "space_remote_records" table after copying rows
DROP TABLE `space_remote_records`;
-- rename temporary table "new_space_remote_records" to "space_remote_records"
ALTER TABLE `new_space_remote_records` RENAME TO `space_remote_records`;
-- create index "idx_space_remote_records_seq" to table: "space_remote_records"
CREATE INDEX `idx_space_remote_records_seq` ON `space_remote_records` (`seq`);
-- create "new_space_remote_syncs" table
CREATE TABLE `new_space_remote_syncs` (
  `space` text NULL,
  `space_owner` text NULL,
  `space_type` text NULL,
  `space_skey` text NULL,
  `repo` text NULL,
  `rev` text NULL,
  `state` blob NULL,
  `updated_at` datetime NULL,
  PRIMARY KEY (`space_owner`, `space_type`, `space_skey`, `repo`)
);
-- copy rows from old table "space_remote_syncs" to new temporary table "new_space_remote_syncs"
INSERT INTO `new_space_remote_syncs` (`space`, `space_owner`, `space_type`, `space_skey`, `repo`, `rev`, `state`, `updated_at`) SELECT `space`, `space_owner`, `space_type`, `space_skey`, `repo`, `rev`, `state`, `updated_at` FROM `space_remote_syncs`;
-- drop "space_remote_syncs" table after copying rows
DROP TABLE `space_remote_syncs`;
-- rename temporary table "new_space_remote_syncs" to "space_remote_syncs"
ALTER TABLE `new_space_remote_syncs` RENAME TO `space_remote_syncs`;
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;

-- +goose Down
-- reverse: create "new_space_remote_syncs" table
DROP TABLE `new_space_remote_syncs`;
-- reverse: create index "idx_space_remote_records_seq" to table: "space_remote_records"
DROP INDEX `idx_space_remote_records_seq`;
-- reverse: create "new_space_remote_records" table
DROP TABLE `new_space_remote_records`;
-- reverse: create "new_space_webhooks" table
DROP TABLE `new_space_webhooks`;
-- reverse: create index "idx_space_deleted_deliveries_retry_at" to table: "space_deleted_deliveries"
DROP INDEX `idx_space_deleted_deliveries_retry_at`;
-- reverse: create "new_space_deleted_deliveries" table
DROP TABLE `new_space_deleted_deliveries`;
-- reverse: create "new_blob_refs" table
DROP TABLE `new_blob_refs`;
-- reverse: create "new_registrations" table
DROP TABLE `new_registrations`;
-- reverse: create "new_space_repos" table
DROP TABLE `new_space_repos`;
-- reverse: create index "idx_space_records_rev" to table: "space_records"
DROP INDEX `idx_space_records_rev`;
-- reverse: create index "idx_space_records_seq" to table: "space_records"
DROP INDEX `idx_space_records_seq`;
-- reverse: create "new_space_records" table
DROP TABLE `new_space_records`;
