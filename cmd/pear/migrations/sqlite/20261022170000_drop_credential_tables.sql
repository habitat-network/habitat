-- +goose Up
-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- drop "google_credentials_models" table
DROP TABLE `google_credentials_models`;
-- drop "pds_credentials_models" table
DROP TABLE `pds_credentials_models`;
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;

-- +goose Down
-- reverse: drop "pds_credentials_models" table
CREATE TABLE `pds_credentials_models` (
  `did` text NULL,
  `access_token` text NULL,
  `refresh_token` text NULL,
  `token_type` text NULL,
  `scope` text NULL,
  `dpop_key` blob NULL,
  `created_at` datetime NULL,
  `updated_at` datetime NULL,
  PRIMARY KEY (`did`)
);
-- reverse: drop "google_credentials_models" table
CREATE TABLE `google_credentials_models` (
  `email` text NULL,
  `access_token` text NULL,
  `refresh_token` text NULL,
  `expiry` datetime NULL,
  `id_token` text NULL,
  `created_at` datetime NULL,
  `updated_at` datetime NULL,
  PRIMARY KEY (`email`)
);
