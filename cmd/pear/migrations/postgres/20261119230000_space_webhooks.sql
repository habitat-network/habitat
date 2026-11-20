-- +goose Up
-- create "space_webhooks" table
CREATE TABLE "space_webhooks" (
  "space" text NOT NULL,
  "endpoint" text NOT NULL,
  "token" text NOT NULL,
  "expires_at" timestamptz NULL,
  "expiry_notified" boolean NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("space", "endpoint", "token")
);

-- +goose Down
-- reverse: create "space_webhooks" table
DROP TABLE "space_webhooks";
