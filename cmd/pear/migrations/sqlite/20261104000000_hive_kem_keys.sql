-- +goose Up
-- add column "kem_public_key" to table: "idents"
ALTER TABLE `idents` ADD COLUMN `kem_public_key` text NULL;
-- add column "kem_private_key_enc" to table: "idents"
ALTER TABLE `idents` ADD COLUMN `kem_private_key_enc` text NULL;

-- +goose Down
-- reverse: add column "kem_private_key_enc" to table: "idents"
ALTER TABLE `idents` DROP COLUMN `kem_private_key_enc`;
-- reverse: add column "kem_public_key" to table: "idents"
ALTER TABLE `idents` DROP COLUMN `kem_public_key`;
