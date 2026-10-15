-- +goose Up
-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- drop "o_auth_requests" table
DROP TABLE `o_auth_requests`;
-- create "key_values" table
CREATE TABLE `key_values` (
  `namespace` text NULL,
  `key` text NULL,
  `value` blob NOT NULL,
  `expires_at` datetime NOT NULL,
  PRIMARY KEY (`namespace`, `key`)
);
-- create index "idx_key_values_expires_at" to table: "key_values"
CREATE INDEX `idx_key_values_expires_at` ON `key_values` (`expires_at`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;

-- +goose Down
-- reverse: create index "idx_key_values_expires_at" to table: "key_values"
DROP INDEX `idx_key_values_expires_at`;
-- reverse: create "key_values" table
DROP TABLE `key_values`;
-- reverse: drop "o_auth_requests" table
CREATE TABLE `o_auth_requests` (
  `key` text NULL,
  `client_id` text NULL,
  `subject` text NULL,
  `scopes` text NULL,
  `code_challenge` text NULL,
  `code_challenge_method` text NULL,
  `redirect_uri` text NULL,
  `state` text NULL,
  `response_type` text NULL,
  `response_mode` text NULL,
  `expires_at` datetime NULL,
  PRIMARY KEY (`key`)
);
CREATE INDEX `idx_o_auth_requests_expires_at` ON `o_auth_requests` (`expires_at`);
