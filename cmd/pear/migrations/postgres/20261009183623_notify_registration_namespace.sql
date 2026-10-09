-- +goose Up
-- modify "registrations" table
ALTER TABLE "registrations" ADD COLUMN "namespace" text NULL DEFAULT 'network.habitat.space';

-- +goose Down
-- reverse: modify "registrations" table
ALTER TABLE "registrations" DROP COLUMN "namespace";
