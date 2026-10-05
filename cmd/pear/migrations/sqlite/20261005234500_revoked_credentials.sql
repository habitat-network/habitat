-- +goose Up
-- create "revoked_credentials" table
CREATE TABLE `revoked_credentials` (
  `space` text NULL,
  `jti` text NULL,
  `retain_until` datetime NULL,
  PRIMARY KEY (`space`, `jti`)
);
-- create index "idx_revoked_credentials_retain_until" to table: "revoked_credentials"
CREATE INDEX `idx_revoked_credentials_retain_until` ON `revoked_credentials` (`retain_until`);

-- +goose Down
-- reverse: create index "idx_revoked_credentials_retain_until" to table: "revoked_credentials"
DROP INDEX `idx_revoked_credentials_retain_until`;
-- reverse: create "revoked_credentials" table
DROP TABLE `revoked_credentials`;
