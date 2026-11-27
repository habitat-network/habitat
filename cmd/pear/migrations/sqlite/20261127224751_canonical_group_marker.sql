-- +goose Up
-- create "canonical_group_rows" table
CREATE TABLE `canonical_group_rows` (
  `did` text NOT NULL,
  PRIMARY KEY (`did`)
);

-- +goose Down
-- reverse: create "canonical_group_rows" table
DROP TABLE `canonical_group_rows`;
