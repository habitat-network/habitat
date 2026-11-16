-- +goose Up
-- modify "space_repos" table
ALTER TABLE "space_repos" ADD COLUMN "removed" boolean NULL;

-- +goose Down
-- reverse: modify "space_repos" table
ALTER TABLE "space_repos" DROP COLUMN "removed";
