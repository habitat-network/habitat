-- +goose Up
-- modify "blob_refs" table
ALTER TABLE "blob_refs" DROP CONSTRAINT "blob_refs_pkey", ALTER COLUMN "space" DROP NOT NULL, ALTER COLUMN "space_owner" SET NOT NULL, ALTER COLUMN "space_type" SET NOT NULL, ALTER COLUMN "space_skey" SET NOT NULL, ADD PRIMARY KEY ("cid", "space_owner", "space_type", "space_skey", "repo", "collection", "rkey");
-- modify "registrations" table
ALTER TABLE "registrations" DROP CONSTRAINT "registrations_pkey", ALTER COLUMN "space" DROP NOT NULL, ALTER COLUMN "space_owner" SET NOT NULL, ALTER COLUMN "space_type" SET NOT NULL, ALTER COLUMN "space_skey" SET NOT NULL, ADD PRIMARY KEY ("space_owner", "space_type", "space_skey", "repo", "audience");
-- modify "space_deleted_deliveries" table
ALTER TABLE "space_deleted_deliveries" DROP CONSTRAINT "space_deleted_deliveries_pkey", ALTER COLUMN "space" DROP NOT NULL, ALTER COLUMN "space_owner" SET NOT NULL, ALTER COLUMN "space_type" SET NOT NULL, ALTER COLUMN "space_skey" SET NOT NULL, ADD PRIMARY KEY ("space_owner", "space_type", "space_skey", "audience");
-- modify "space_records" table
ALTER TABLE "space_records" DROP CONSTRAINT "space_records_pkey", ALTER COLUMN "space" DROP NOT NULL, ALTER COLUMN "space_owner" SET NOT NULL, ALTER COLUMN "space_type" SET NOT NULL, ALTER COLUMN "space_skey" SET NOT NULL, ADD PRIMARY KEY ("space_owner", "space_type", "space_skey", "repo", "collection", "rkey");
-- modify "space_remote_records" table
ALTER TABLE "space_remote_records" DROP CONSTRAINT "space_remote_records_pkey", ALTER COLUMN "space" DROP NOT NULL, ALTER COLUMN "space_owner" SET NOT NULL, ALTER COLUMN "space_type" SET NOT NULL, ALTER COLUMN "space_skey" SET NOT NULL, ADD PRIMARY KEY ("space_owner", "space_type", "space_skey", "repo", "collection", "rkey");
-- modify "space_remote_syncs" table
ALTER TABLE "space_remote_syncs" DROP CONSTRAINT "space_remote_syncs_pkey", ALTER COLUMN "space" DROP NOT NULL, ALTER COLUMN "space_owner" SET NOT NULL, ALTER COLUMN "space_type" SET NOT NULL, ALTER COLUMN "space_skey" SET NOT NULL, ADD PRIMARY KEY ("space_owner", "space_type", "space_skey", "repo");
-- modify "space_repos" table
ALTER TABLE "space_repos" DROP CONSTRAINT "space_repos_pkey", ALTER COLUMN "space" DROP NOT NULL, ALTER COLUMN "space_owner" SET NOT NULL, ALTER COLUMN "space_type" SET NOT NULL, ALTER COLUMN "space_skey" SET NOT NULL, ADD PRIMARY KEY ("space_owner", "space_type", "space_skey", "repo");
-- modify "space_webhooks" table
ALTER TABLE "space_webhooks" DROP CONSTRAINT "space_webhooks_pkey", ALTER COLUMN "space" DROP NOT NULL, ALTER COLUMN "space_owner" SET NOT NULL, ALTER COLUMN "space_type" SET NOT NULL, ALTER COLUMN "space_skey" SET NOT NULL, ADD PRIMARY KEY ("space_owner", "space_type", "space_skey", "endpoint", "token");

-- +goose Down
-- reverse: modify "space_webhooks" table
ALTER TABLE "space_webhooks" DROP CONSTRAINT "space_webhooks_pkey", ALTER COLUMN "space_skey" DROP NOT NULL, ALTER COLUMN "space_type" DROP NOT NULL, ALTER COLUMN "space_owner" DROP NOT NULL, ALTER COLUMN "space" SET NOT NULL, ADD PRIMARY KEY ("space", "endpoint", "token");
-- reverse: modify "space_repos" table
ALTER TABLE "space_repos" DROP CONSTRAINT "space_repos_pkey", ALTER COLUMN "space_skey" DROP NOT NULL, ALTER COLUMN "space_type" DROP NOT NULL, ALTER COLUMN "space_owner" DROP NOT NULL, ALTER COLUMN "space" SET NOT NULL, ADD PRIMARY KEY ("space", "repo");
-- reverse: modify "space_remote_syncs" table
ALTER TABLE "space_remote_syncs" DROP CONSTRAINT "space_remote_syncs_pkey", ALTER COLUMN "space_skey" DROP NOT NULL, ALTER COLUMN "space_type" DROP NOT NULL, ALTER COLUMN "space_owner" DROP NOT NULL, ALTER COLUMN "space" SET NOT NULL, ADD PRIMARY KEY ("space", "repo");
-- reverse: modify "space_remote_records" table
ALTER TABLE "space_remote_records" DROP CONSTRAINT "space_remote_records_pkey", ALTER COLUMN "space_skey" DROP NOT NULL, ALTER COLUMN "space_type" DROP NOT NULL, ALTER COLUMN "space_owner" DROP NOT NULL, ALTER COLUMN "space" SET NOT NULL, ADD PRIMARY KEY ("space", "repo", "collection", "rkey");
-- reverse: modify "space_records" table
ALTER TABLE "space_records" DROP CONSTRAINT "space_records_pkey", ALTER COLUMN "space_skey" DROP NOT NULL, ALTER COLUMN "space_type" DROP NOT NULL, ALTER COLUMN "space_owner" DROP NOT NULL, ALTER COLUMN "space" SET NOT NULL, ADD PRIMARY KEY ("space", "repo", "collection", "rkey");
-- reverse: modify "space_deleted_deliveries" table
ALTER TABLE "space_deleted_deliveries" DROP CONSTRAINT "space_deleted_deliveries_pkey", ALTER COLUMN "space_skey" DROP NOT NULL, ALTER COLUMN "space_type" DROP NOT NULL, ALTER COLUMN "space_owner" DROP NOT NULL, ALTER COLUMN "space" SET NOT NULL, ADD PRIMARY KEY ("space", "audience");
-- reverse: modify "registrations" table
ALTER TABLE "registrations" DROP CONSTRAINT "registrations_pkey", ALTER COLUMN "space_skey" DROP NOT NULL, ALTER COLUMN "space_type" DROP NOT NULL, ALTER COLUMN "space_owner" DROP NOT NULL, ALTER COLUMN "space" SET NOT NULL, ADD PRIMARY KEY ("space", "repo", "audience");
-- reverse: modify "blob_refs" table
ALTER TABLE "blob_refs" DROP CONSTRAINT "blob_refs_pkey", ALTER COLUMN "space_skey" DROP NOT NULL, ALTER COLUMN "space_type" DROP NOT NULL, ALTER COLUMN "space_owner" DROP NOT NULL, ALTER COLUMN "space" SET NOT NULL, ADD PRIMARY KEY ("cid", "space", "repo", "collection", "rkey");
