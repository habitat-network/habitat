-- +goose Up
-- drop column "value" from table: "space_records"
ALTER TABLE "space_records" DROP COLUMN "value";

-- +goose Down
-- The CBOR values are not restored; value_json holds the data.
ALTER TABLE "space_records" ADD COLUMN "value" bytea NULL;
