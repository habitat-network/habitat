-- +goose Up
-- create "space_webhooks" table
CREATE TABLE `space_webhooks` (
  `space` text NULL,
  `endpoint` text NULL,
  `token` text NULL,
  `expires_at` datetime NULL,
  `expiry_notified` numeric NULL,
  `created_at` datetime NULL,
  `updated_at` datetime NULL,
  PRIMARY KEY (`space`, `endpoint`, `token`)
);

-- +goose Down
-- reverse: create "space_webhooks" table
DROP TABLE `space_webhooks`;
