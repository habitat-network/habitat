-- +goose Up
-- create "blob_refs" table
CREATE TABLE `blob_refs` (
  `cid` text NULL,
  `space` text NULL,
  `repo` text NULL,
  `collection` text NULL,
  `rkey` text NULL,
  PRIMARY KEY (`cid`, `space`, `repo`, `collection`, `rkey`)
);

-- +goose Down
-- reverse: create "blob_refs" table
DROP TABLE `blob_refs`;
