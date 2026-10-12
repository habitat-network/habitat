-- +goose Up
-- add column "namespace" to table: "registrations"
ALTER TABLE `registrations` ADD COLUMN `namespace` text NULL DEFAULT 'network.habitat.space';

-- +goose Down
-- reverse: add column "namespace" to table: "registrations"
ALTER TABLE `registrations` DROP COLUMN `namespace`;
