-- +goose Up
-- add column "policy" to table: "spaces"
ALTER TABLE `spaces` ADD COLUMN `policy` blob NULL;

-- +goose Down
-- reverse: add column "policy" to table: "spaces"
ALTER TABLE `spaces` DROP COLUMN `policy`;
