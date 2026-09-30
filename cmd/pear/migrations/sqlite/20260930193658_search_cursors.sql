-- +goose Up
-- create "search_cursors" table
CREATE TABLE `search_cursors` (
  `space` text NULL,
  `repo` text NULL,
  `rev` text NULL,
  PRIMARY KEY (`space`, `repo`)
);

-- +goose Down
-- reverse: create "search_cursors" table
DROP TABLE `search_cursors`;
