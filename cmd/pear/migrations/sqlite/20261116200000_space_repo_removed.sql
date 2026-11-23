-- +goose Up
-- add column "removed" to table: "space_repos"
ALTER TABLE `space_repos` ADD COLUMN `removed` numeric NULL;

-- +goose Down
-- reverse: add column "removed" to table: "space_repos"
ALTER TABLE `space_repos` DROP COLUMN `removed`;
