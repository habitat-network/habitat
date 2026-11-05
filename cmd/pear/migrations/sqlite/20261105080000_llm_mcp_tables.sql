-- +goose Up
-- create "llm_connections" table
CREATE TABLE `llm_connections` (
  `org_did` text NULL,
  `name` text NULL,
  `description` text NOT NULL DEFAULT '',
  `provider` text NOT NULL,
  `model` text NOT NULL,
  `base_url` text NOT NULL DEFAULT '',
  `api_key_enc` text NOT NULL DEFAULT '',
  `created_at` datetime NOT NULL,
  `updated_at` datetime NOT NULL,
  PRIMARY KEY (`org_did`, `name`)
);
-- create "mcp_servers" table
CREATE TABLE `mcp_servers` (
  `org_did` text NULL,
  `name` text NULL,
  `description` text NOT NULL DEFAULT '',
  `url` text NOT NULL,
  `auth_type` text NOT NULL,
  `generation` text NOT NULL,
  `oauth_resource` text NOT NULL DEFAULT '',
  `oauth_issuer` text NOT NULL DEFAULT '',
  `oauth_auth_url` text NOT NULL DEFAULT '',
  `oauth_token_url` text NOT NULL DEFAULT '',
  `oauth_iss_parameter_supported` numeric NOT NULL DEFAULT false,
  `oauth_scopes` text NOT NULL DEFAULT '',
  `oauth_client_id` text NOT NULL DEFAULT '',
  `oauth_auth_style` integer NOT NULL DEFAULT 0,
  `oauth_client_secret_enc` text NOT NULL DEFAULT '',
  `created_at` datetime NOT NULL,
  `updated_at` datetime NOT NULL,
  PRIMARY KEY (`org_did`, `name`)
);

-- +goose Down
-- reverse: create "mcp_servers" table
DROP TABLE `mcp_servers`;
-- reverse: create "llm_connections" table
DROP TABLE `llm_connections`;
