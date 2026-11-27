-- +goose Up
-- modify "blob_refs" table
ALTER TABLE "blob_refs" DROP COLUMN "space";
-- modify "registrations" table
ALTER TABLE "registrations" DROP COLUMN "space";
-- modify "space_deleted_deliveries" table
ALTER TABLE "space_deleted_deliveries" DROP COLUMN "space";
-- modify "space_records" table
ALTER TABLE "space_records" DROP COLUMN "space";
-- modify "space_remote_records" table
ALTER TABLE "space_remote_records" DROP COLUMN "space";
-- modify "space_remote_syncs" table
ALTER TABLE "space_remote_syncs" DROP COLUMN "space";
-- modify "space_repos" table
ALTER TABLE "space_repos" DROP COLUMN "space";
-- modify "space_webhooks" table
ALTER TABLE "space_webhooks" DROP COLUMN "space";

-- +goose Down
-- reverse: modify "space_webhooks" table
ALTER TABLE "space_webhooks" ADD COLUMN "space" text NULL;
-- reverse: modify "space_repos" table
ALTER TABLE "space_repos" ADD COLUMN "space" text NULL;
-- reverse: modify "space_remote_syncs" table
ALTER TABLE "space_remote_syncs" ADD COLUMN "space" text NULL;
-- reverse: modify "space_remote_records" table
ALTER TABLE "space_remote_records" ADD COLUMN "space" text NULL;
-- reverse: modify "space_records" table
ALTER TABLE "space_records" ADD COLUMN "space" text NULL;
-- reverse: modify "space_deleted_deliveries" table
ALTER TABLE "space_deleted_deliveries" ADD COLUMN "space" text NULL;
-- reverse: modify "registrations" table
ALTER TABLE "registrations" ADD COLUMN "space" text NULL;
-- reverse: modify "blob_refs" table
ALTER TABLE "blob_refs" ADD COLUMN "space" text NULL;
