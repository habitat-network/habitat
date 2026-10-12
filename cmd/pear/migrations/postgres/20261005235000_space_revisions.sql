-- +goose Up
-- add column "rev" to table: "spaces"
ALTER TABLE "spaces" ADD COLUMN "rev" text NULL;
-- add column "space_rev" to table: "space_repos"
ALTER TABLE "space_repos" ADD COLUMN "space_rev" text NULL;
-- Backfill the space revision sequence from the repo revisions already stored:
-- each repo starts at its own rev, and a space starts at its newest repo's rev,
-- so syncers that listRepos?since=<space rev> never see a rev older than data
-- that predates this migration.
UPDATE "space_repos" SET "space_rev" = "rev";
UPDATE "spaces" SET "rev" = (
  SELECT MAX(r."rev") FROM "space_repos" r
  WHERE r."space" = 'at://' || "spaces"."owner" || '/space/' || "spaces"."type" || '/' || "spaces"."skey"
);

-- +goose Down
ALTER TABLE "space_repos" DROP COLUMN "space_rev";
ALTER TABLE "spaces" DROP COLUMN "rev";
