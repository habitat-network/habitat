-- +goose Up
-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- drop "clique_members" table
DROP TABLE `clique_members`;
-- drop "password_entries" table
DROP TABLE `password_entries`;
-- drop "organizations" table
DROP TABLE `organizations`;
-- drop "members" table
DROP TABLE `members`;
-- drop "permissions" table
DROP TABLE `permissions`;
-- drop "records" table
DROP TABLE `records`;
-- drop "blobs" table
DROP TABLE `blobs`;
-- drop "links" table
DROP TABLE `links`;
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;

-- +goose Down
-- reverse: drop "links" table
CREATE TABLE `links` (
  `cid` text NULL,
  `did` text NULL,
  `ref` text NULL,
  PRIMARY KEY (`cid`, `did`, `ref`)
);
-- reverse: drop "blobs" table
CREATE TABLE `blobs` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `created_at` datetime NULL,
  `updated_at` datetime NULL,
  `deleted_at` datetime NULL,
  `did` text NULL,
  `cid` text NULL,
  `mime_type` text NULL,
  `blob` blob NULL
);
CREATE INDEX `idx_blobs_deleted_at` ON `blobs` (`deleted_at`);
-- reverse: drop "records" table
CREATE TABLE `records` (
  `did` text NULL,
  `collection` text NULL,
  `rkey` text NULL,
  `value` blob NULL,
  `created_at` datetime NULL,
  `updated_at` datetime NULL,
  PRIMARY KEY (`did`, `collection`, `rkey`)
);
-- reverse: drop "permissions" table
CREATE TABLE `permissions` (
  `grantee` text NULL,
  `owner` text NULL,
  `collection` text NULL,
  `rkey` text NULL,
  `created_at` datetime NULL,
  `updated_at` datetime NULL,
  PRIMARY KEY (`grantee`, `owner`, `collection`, `rkey`)
);
-- reverse: drop "members" table
CREATE TABLE `members` (
  `org_id` text NULL,
  `did` text NULL,
  `role` text NOT NULL,
  `login_id` text NOT NULL,
  `created_at` datetime NULL,
  PRIMARY KEY (`org_id`, `did`),
  CONSTRAINT `fk_members_organization` FOREIGN KEY (`org_id`) REFERENCES `organizations` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION
);
CREATE INDEX `idx_members_login_id` ON `members` (`login_id`);
-- reverse: drop "organizations" table
CREATE TABLE `organizations` (
  `id` text NULL,
  `name` text NULL,
  `login_method` text NULL,
  `signing_secret` text NULL,
  `created_at` datetime NULL,
  `handle_subdomain` text NULL,
  `contact_email` text NULL,
  PRIMARY KEY (`id`)
);
CREATE UNIQUE INDEX `organizations_handle_subdomain` ON `organizations` (`handle_subdomain`);
-- reverse: drop "password_entries" table
CREATE TABLE `password_entries` (
  `did` text NULL,
  `password` text NULL,
  PRIMARY KEY (`did`)
);
-- reverse: drop "clique_members" table
CREATE TABLE `clique_members` (
  `owner` text NULL,
  `key` text NULL,
  `member` text NULL,
  `created_at` datetime NULL,
  PRIMARY KEY (`owner`, `key`, `member`)
);
