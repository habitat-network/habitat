-- +goose Up
-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- drop "instance_invites" table
DROP TABLE `instance_invites`;
-- drop "o_auth_sessions" table
DROP TABLE `o_auth_sessions`;
-- drop "spent_tokens" table
DROP TABLE `spent_tokens`;
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;

-- +goose Down
-- reverse: drop "spent_tokens" table
CREATE TABLE `spent_tokens` (
  `org_id` text NULL,
  `jti` text NULL,
  `consumed_at` datetime NOT NULL,
  PRIMARY KEY (`org_id`, `jti`)
);
-- reverse: drop "o_auth_sessions" table
CREATE TABLE `o_auth_sessions` (
  `signature` text NULL,
  `client_id` text NULL,
  `subject` text NULL,
  `scopes` text NULL,
  `expires_at` datetime NULL,
  PRIMARY KEY (`signature`)
);
-- reverse: drop "instance_invites" table
CREATE TABLE `instance_invites` (
  `token` text NULL,
  `created_at` datetime NULL,
  `used_at` datetime NULL,
  PRIMARY KEY (`token`)
);
