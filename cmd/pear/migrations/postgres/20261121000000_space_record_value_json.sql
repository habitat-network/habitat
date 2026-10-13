-- +goose Up
-- add column "value_json" to table: "space_records"
ALTER TABLE "space_records" ADD COLUMN "value_json" jsonb NULL;

-- +goose Down
ALTER TABLE "space_records" DROP COLUMN "value_json";
