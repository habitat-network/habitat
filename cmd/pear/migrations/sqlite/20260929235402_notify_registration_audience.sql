-- +goose Up
-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_registrations" table
CREATE TABLE `new_registrations` (
  `space` text NULL,
  `repo` text NULL,
  `audience` text NULL,
  `endpoint` text NULL,
  `expires_at` datetime NULL,
  `created_at` datetime NULL,
  `updated_at` datetime NULL,
  PRIMARY KEY (`space`, `repo`, `audience`)
);
-- copy rows from old table "registrations" to new temporary table "new_registrations",
-- taking each row's audience from the endpoint it was already being addressed by
INSERT INTO `new_registrations` (`space`, `repo`, `audience`, `endpoint`, `expires_at`, `created_at`, `updated_at`) SELECT `space`, `repo`, `endpoint`, `endpoint`, `expires_at`, `created_at`, `updated_at` FROM `registrations`;
-- drop "registrations" table after copying rows
DROP TABLE `registrations`;
-- rename temporary table "new_registrations" to "registrations"
ALTER TABLE `new_registrations` RENAME TO `registrations`;
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;

-- +goose Down
-- reverse: restore the endpoint-keyed table the same way, through a temporary one
PRAGMA foreign_keys = off;
CREATE TABLE `new_registrations` (
  `space` text NULL,
  `repo` text NULL,
  `endpoint` text NULL,
  `expires_at` datetime NULL,
  `created_at` datetime NULL,
  `updated_at` datetime NULL,
  PRIMARY KEY (`space`, `repo`, `endpoint`)
);
INSERT INTO `new_registrations` (`space`, `repo`, `endpoint`, `expires_at`, `created_at`, `updated_at`) SELECT `space`, `repo`, `endpoint`, `expires_at`, `created_at`, `updated_at` FROM `registrations`;
DROP TABLE `registrations`;
ALTER TABLE `new_registrations` RENAME TO `registrations`;
PRAGMA foreign_keys = on;
