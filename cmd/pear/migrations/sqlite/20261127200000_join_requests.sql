-- +goose Up
-- create "join_requests" table
CREATE TABLE `join_requests` (
  `org_id` text NULL,
  `member` text NULL,
  `message` text NOT NULL,
  `created_at` datetime NOT NULL,
  PRIMARY KEY (`org_id`, `member`)
);

-- +goose Down
-- reverse: create "join_requests" table
DROP TABLE `join_requests`;
