-- +goose Up
-- drop "google_credentials_models" table
DROP TABLE "google_credentials_models";
-- drop "pds_credentials_models" table
DROP TABLE "pds_credentials_models";

-- +goose Down
-- reverse: drop "pds_credentials_models" table
CREATE TABLE "pds_credentials_models" (
  "did" text NOT NULL,
  "access_token" text NULL,
  "refresh_token" text NULL,
  "token_type" text NULL,
  "scope" text NULL,
  "dpop_key" bytea NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("did")
);
-- reverse: drop "google_credentials_models" table
CREATE TABLE "google_credentials_models" (
  "email" text NOT NULL,
  "access_token" text NULL,
  "refresh_token" text NULL,
  "expiry" timestamptz NULL,
  "id_token" text NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("email")
);
