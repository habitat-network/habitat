-- +goose Up
-- add column "seq" to table: "space_repos"
ALTER TABLE `space_repos` ADD COLUMN `seq` integer NOT NULL DEFAULT 0;
-- create index "idx_space_repos_seq" to table: "space_repos"
CREATE INDEX `idx_space_repos_seq` ON `space_repos` (`seq`);

-- +goose Down
-- reverse: create index "idx_space_repos_seq" to table: "space_repos"
DROP INDEX `idx_space_repos_seq`;
-- reverse: add column "seq" to table: "space_repos"
ALTER TABLE `space_repos` DROP COLUMN `seq`;
