-- +goose Up
-- Backfill before constraining: `audience` cannot be added as NOT NULL to a
-- table that already has rows, and the audience of every existing registration is
-- the endpoint URL it was already being addressed by.
ALTER TABLE "registrations" ADD COLUMN "audience" text;
UPDATE "registrations" SET "audience" = "endpoint" WHERE "audience" IS NULL;
ALTER TABLE "registrations" ALTER COLUMN "audience" SET NOT NULL;
-- A registration is identified by what deliveries are addressed to, not by
-- where the call goes, so the key moves to `audience` and `endpoint` becomes a
-- plain column: a service that starts resolving elsewhere updates in place, and
-- two subscribers sharing an address stay distinct.
ALTER TABLE "registrations" DROP CONSTRAINT "registrations_pkey";
ALTER TABLE "registrations" ADD PRIMARY KEY ("space", "repo", "audience");
ALTER TABLE "registrations" ALTER COLUMN "endpoint" DROP NOT NULL;

-- +goose Down
-- `endpoint` still holds each row's delivery address, so dropping `audience`
-- restores the previous shape without touching any other column.
ALTER TABLE "registrations" DROP CONSTRAINT "registrations_pkey";
ALTER TABLE "registrations" DROP COLUMN "audience";
ALTER TABLE "registrations" ALTER COLUMN "endpoint" SET NOT NULL;
ALTER TABLE "registrations" ADD PRIMARY KEY ("space", "repo", "endpoint");
