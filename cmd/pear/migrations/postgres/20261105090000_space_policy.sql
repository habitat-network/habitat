-- +goose Up
-- modify "spaces" table
ALTER TABLE "spaces" ADD COLUMN "policy" bytea NULL;

-- +goose Down
-- reverse: modify "spaces" table
ALTER TABLE "spaces" DROP COLUMN "policy";
