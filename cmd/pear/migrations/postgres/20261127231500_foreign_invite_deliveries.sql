-- +goose Up
CREATE TABLE "foreign_invite_deliveries" (
  "space_owner" text NOT NULL,
  "space_type" text NOT NULL,
  "space_skey" text NOT NULL,
  "author" text NOT NULL,
  "rev" text NOT NULL,
  "digest" bytea NOT NULL,
  "ready_at" bigint NOT NULL,
  "lease_until" bigint NOT NULL,
  "expires_at" bigint NOT NULL,
  "attempts" bigint NOT NULL,
  PRIMARY KEY ("space_owner", "space_type", "space_skey", "author")
);
CREATE INDEX "idx_foreign_invite_deliveries_expires_at" ON "foreign_invite_deliveries" ("expires_at");
CREATE INDEX "idx_foreign_invite_deliveries_ready_at" ON "foreign_invite_deliveries" ("ready_at");

-- +goose Down
DROP TABLE "foreign_invite_deliveries";
