-- +goose Up
-- add column "space_owner" to table: "space_records"
ALTER TABLE `space_records` ADD COLUMN `space_owner` text NULL;
-- add column "space_type" to table: "space_records"
ALTER TABLE `space_records` ADD COLUMN `space_type` text NULL;
-- add column "space_skey" to table: "space_records"
ALTER TABLE `space_records` ADD COLUMN `space_skey` text NULL;
-- add column "space_owner" to table: "space_repos"
ALTER TABLE `space_repos` ADD COLUMN `space_owner` text NULL;
-- add column "space_type" to table: "space_repos"
ALTER TABLE `space_repos` ADD COLUMN `space_type` text NULL;
-- add column "space_skey" to table: "space_repos"
ALTER TABLE `space_repos` ADD COLUMN `space_skey` text NULL;
-- add column "space_owner" to table: "registrations"
ALTER TABLE `registrations` ADD COLUMN `space_owner` text NULL;
-- add column "space_type" to table: "registrations"
ALTER TABLE `registrations` ADD COLUMN `space_type` text NULL;
-- add column "space_skey" to table: "registrations"
ALTER TABLE `registrations` ADD COLUMN `space_skey` text NULL;
-- add column "space_owner" to table: "blob_refs"
ALTER TABLE `blob_refs` ADD COLUMN `space_owner` text NULL;
-- add column "space_type" to table: "blob_refs"
ALTER TABLE `blob_refs` ADD COLUMN `space_type` text NULL;
-- add column "space_skey" to table: "blob_refs"
ALTER TABLE `blob_refs` ADD COLUMN `space_skey` text NULL;
-- add column "space_owner" to table: "space_deleted_deliveries"
ALTER TABLE `space_deleted_deliveries` ADD COLUMN `space_owner` text NULL;
-- add column "space_type" to table: "space_deleted_deliveries"
ALTER TABLE `space_deleted_deliveries` ADD COLUMN `space_type` text NULL;
-- add column "space_skey" to table: "space_deleted_deliveries"
ALTER TABLE `space_deleted_deliveries` ADD COLUMN `space_skey` text NULL;
-- add column "space_owner" to table: "space_webhooks"
ALTER TABLE `space_webhooks` ADD COLUMN `space_owner` text NULL;
-- add column "space_type" to table: "space_webhooks"
ALTER TABLE `space_webhooks` ADD COLUMN `space_type` text NULL;
-- add column "space_skey" to table: "space_webhooks"
ALTER TABLE `space_webhooks` ADD COLUMN `space_skey` text NULL;
-- add column "space_owner" to table: "space_remote_records"
ALTER TABLE `space_remote_records` ADD COLUMN `space_owner` text NULL;
-- add column "space_type" to table: "space_remote_records"
ALTER TABLE `space_remote_records` ADD COLUMN `space_type` text NULL;
-- add column "space_skey" to table: "space_remote_records"
ALTER TABLE `space_remote_records` ADD COLUMN `space_skey` text NULL;
-- add column "space_owner" to table: "space_remote_syncs"
ALTER TABLE `space_remote_syncs` ADD COLUMN `space_owner` text NULL;
-- add column "space_type" to table: "space_remote_syncs"
ALTER TABLE `space_remote_syncs` ADD COLUMN `space_type` text NULL;
-- add column "space_skey" to table: "space_remote_syncs"
ALTER TABLE `space_remote_syncs` ADD COLUMN `space_skey` text NULL;

-- +goose Down
-- reverse: add column "space_skey" to table: "space_remote_syncs"
ALTER TABLE `space_remote_syncs` DROP COLUMN `space_skey`;
-- reverse: add column "space_type" to table: "space_remote_syncs"
ALTER TABLE `space_remote_syncs` DROP COLUMN `space_type`;
-- reverse: add column "space_owner" to table: "space_remote_syncs"
ALTER TABLE `space_remote_syncs` DROP COLUMN `space_owner`;
-- reverse: add column "space_skey" to table: "space_remote_records"
ALTER TABLE `space_remote_records` DROP COLUMN `space_skey`;
-- reverse: add column "space_type" to table: "space_remote_records"
ALTER TABLE `space_remote_records` DROP COLUMN `space_type`;
-- reverse: add column "space_owner" to table: "space_remote_records"
ALTER TABLE `space_remote_records` DROP COLUMN `space_owner`;
-- reverse: add column "space_skey" to table: "space_webhooks"
ALTER TABLE `space_webhooks` DROP COLUMN `space_skey`;
-- reverse: add column "space_type" to table: "space_webhooks"
ALTER TABLE `space_webhooks` DROP COLUMN `space_type`;
-- reverse: add column "space_owner" to table: "space_webhooks"
ALTER TABLE `space_webhooks` DROP COLUMN `space_owner`;
-- reverse: add column "space_skey" to table: "space_deleted_deliveries"
ALTER TABLE `space_deleted_deliveries` DROP COLUMN `space_skey`;
-- reverse: add column "space_type" to table: "space_deleted_deliveries"
ALTER TABLE `space_deleted_deliveries` DROP COLUMN `space_type`;
-- reverse: add column "space_owner" to table: "space_deleted_deliveries"
ALTER TABLE `space_deleted_deliveries` DROP COLUMN `space_owner`;
-- reverse: add column "space_skey" to table: "blob_refs"
ALTER TABLE `blob_refs` DROP COLUMN `space_skey`;
-- reverse: add column "space_type" to table: "blob_refs"
ALTER TABLE `blob_refs` DROP COLUMN `space_type`;
-- reverse: add column "space_owner" to table: "blob_refs"
ALTER TABLE `blob_refs` DROP COLUMN `space_owner`;
-- reverse: add column "space_skey" to table: "registrations"
ALTER TABLE `registrations` DROP COLUMN `space_skey`;
-- reverse: add column "space_type" to table: "registrations"
ALTER TABLE `registrations` DROP COLUMN `space_type`;
-- reverse: add column "space_owner" to table: "registrations"
ALTER TABLE `registrations` DROP COLUMN `space_owner`;
-- reverse: add column "space_skey" to table: "space_repos"
ALTER TABLE `space_repos` DROP COLUMN `space_skey`;
-- reverse: add column "space_type" to table: "space_repos"
ALTER TABLE `space_repos` DROP COLUMN `space_type`;
-- reverse: add column "space_owner" to table: "space_repos"
ALTER TABLE `space_repos` DROP COLUMN `space_owner`;
-- reverse: add column "space_skey" to table: "space_records"
ALTER TABLE `space_records` DROP COLUMN `space_skey`;
-- reverse: add column "space_type" to table: "space_records"
ALTER TABLE `space_records` DROP COLUMN `space_type`;
-- reverse: add column "space_owner" to table: "space_records"
ALTER TABLE `space_records` DROP COLUMN `space_owner`;
