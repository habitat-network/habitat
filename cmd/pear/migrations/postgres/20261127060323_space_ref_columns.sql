-- +goose Up
-- modify "blob_refs" table
ALTER TABLE "blob_refs" ADD COLUMN "space_owner" text NULL, ADD COLUMN "space_type" text NULL, ADD COLUMN "space_skey" text NULL;
-- modify "registrations" table
ALTER TABLE "registrations" ADD COLUMN "space_owner" text NULL, ADD COLUMN "space_type" text NULL, ADD COLUMN "space_skey" text NULL;
-- modify "space_deleted_deliveries" table
ALTER TABLE "space_deleted_deliveries" ADD COLUMN "space_owner" text NULL, ADD COLUMN "space_type" text NULL, ADD COLUMN "space_skey" text NULL;
-- modify "space_records" table
ALTER TABLE "space_records" ADD COLUMN "space_owner" text NULL, ADD COLUMN "space_type" text NULL, ADD COLUMN "space_skey" text NULL;
-- modify "space_remote_records" table
ALTER TABLE "space_remote_records" ADD COLUMN "space_owner" text NULL, ADD COLUMN "space_type" text NULL, ADD COLUMN "space_skey" text NULL;
-- modify "space_remote_syncs" table
ALTER TABLE "space_remote_syncs" ADD COLUMN "space_owner" text NULL, ADD COLUMN "space_type" text NULL, ADD COLUMN "space_skey" text NULL;
-- modify "space_repos" table
ALTER TABLE "space_repos" ADD COLUMN "space_owner" text NULL, ADD COLUMN "space_type" text NULL, ADD COLUMN "space_skey" text NULL;
-- modify "space_webhooks" table
ALTER TABLE "space_webhooks" ADD COLUMN "space_owner" text NULL, ADD COLUMN "space_type" text NULL, ADD COLUMN "space_skey" text NULL;

-- +goose Down
-- reverse: modify "space_webhooks" table
ALTER TABLE "space_webhooks" DROP COLUMN "space_skey", DROP COLUMN "space_type", DROP COLUMN "space_owner";
-- reverse: modify "space_repos" table
ALTER TABLE "space_repos" DROP COLUMN "space_skey", DROP COLUMN "space_type", DROP COLUMN "space_owner";
-- reverse: modify "space_remote_syncs" table
ALTER TABLE "space_remote_syncs" DROP COLUMN "space_skey", DROP COLUMN "space_type", DROP COLUMN "space_owner";
-- reverse: modify "space_remote_records" table
ALTER TABLE "space_remote_records" DROP COLUMN "space_skey", DROP COLUMN "space_type", DROP COLUMN "space_owner";
-- reverse: modify "space_records" table
ALTER TABLE "space_records" DROP COLUMN "space_skey", DROP COLUMN "space_type", DROP COLUMN "space_owner";
-- reverse: modify "space_deleted_deliveries" table
ALTER TABLE "space_deleted_deliveries" DROP COLUMN "space_skey", DROP COLUMN "space_type", DROP COLUMN "space_owner";
-- reverse: modify "registrations" table
ALTER TABLE "registrations" DROP COLUMN "space_skey", DROP COLUMN "space_type", DROP COLUMN "space_owner";
-- reverse: modify "blob_refs" table
ALTER TABLE "blob_refs" DROP COLUMN "space_skey", DROP COLUMN "space_type", DROP COLUMN "space_owner";
