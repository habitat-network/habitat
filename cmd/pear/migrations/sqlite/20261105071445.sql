-- +goose Up
-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- drop "instance_settings" table
DROP TABLE `instance_settings`;
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;

-- +goose Down
-- reverse: drop "instance_settings" table
CREATE TABLE `instance_settings` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `instance_name` text NULL,
  `org_creation_policy` text NULL,
  `signing_secret` text NULL,
  `created_at` datetime NULL
);
