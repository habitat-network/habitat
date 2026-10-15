-- +goose Up
-- create "key_values" table
CREATE TABLE "key_values" (
  "namespace" character varying(255) NOT NULL,
  "key" character varying(255) NOT NULL,
  "value" bytea NOT NULL,
  "expires_at" timestamptz NOT NULL,
  PRIMARY KEY ("namespace", "key")
);
-- create index "idx_key_values_expires_at" to table: "key_values"
CREATE INDEX "idx_key_values_expires_at" ON "key_values" ("expires_at");
-- drop "o_auth_requests" table
DROP TABLE "o_auth_requests";

-- +goose Down
-- reverse: drop "o_auth_requests" table
CREATE TABLE "o_auth_requests" (
  "key" text NOT NULL,
  "client_id" character varying(1024) NULL,
  "subject" character varying(255) NULL,
  "scopes" character varying(512) NULL,
  "code_challenge" character varying(255) NULL,
  "code_challenge_method" character varying(32) NULL,
  "redirect_uri" character varying(1024) NULL,
  "state" character varying(1024) NULL,
  "response_type" character varying(64) NULL,
  "response_mode" character varying(32) NULL,
  "expires_at" timestamptz NULL,
  PRIMARY KEY ("key")
);
CREATE INDEX "idx_o_auth_requests_expires_at" ON "o_auth_requests" ("expires_at");
-- reverse: create index "idx_key_values_expires_at" to table: "key_values"
DROP INDEX "idx_key_values_expires_at";
-- reverse: create "key_values" table
DROP TABLE "key_values";
