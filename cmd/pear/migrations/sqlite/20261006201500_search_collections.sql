-- +goose Up
-- create "search_collections" table
CREATE TABLE `search_collections` (
  `org_did` text NULL,
  `collection` text NULL,
  `created_at` datetime NULL,
  PRIMARY KEY (`org_did`, `collection`)
);

-- +goose Down
-- reverse: create "search_collections" table
DROP TABLE `search_collections`;
