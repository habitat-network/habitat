-- +goose Up
-- create "join_request_rows" table
CREATE TABLE `join_request_rows` (
  `org_id` text NULL,
  `member` text NULL,
  `message` text NOT NULL,
  `created_at` datetime NOT NULL,
  PRIMARY KEY (`org_id`, `member`)
);

-- +goose Down
-- reverse: create "join_request_rows" table
DROP TABLE `join_request_rows`;
