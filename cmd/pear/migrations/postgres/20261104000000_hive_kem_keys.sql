-- +goose Up
-- modify "idents" table
ALTER TABLE "idents" ADD COLUMN "kem_public_key" text NULL, ADD COLUMN "kem_private_key_enc" text NULL;

-- +goose Down
-- reverse: modify "idents" table
ALTER TABLE "idents" DROP COLUMN "kem_private_key_enc", DROP COLUMN "kem_public_key";
