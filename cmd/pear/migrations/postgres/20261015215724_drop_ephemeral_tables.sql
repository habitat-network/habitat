-- +goose Up
-- drop "instance_invites" table
DROP TABLE "instance_invites";
-- drop "o_auth_sessions" table
DROP TABLE "o_auth_sessions";
-- drop "spent_tokens" table
DROP TABLE "spent_tokens";

-- +goose Down
-- reverse: drop "spent_tokens" table
CREATE TABLE "spent_tokens" (
  "org_id" text NOT NULL,
  "jti" text NOT NULL,
  "consumed_at" timestamptz NOT NULL,
  PRIMARY KEY ("org_id", "jti")
);
-- reverse: drop "o_auth_sessions" table
CREATE TABLE "o_auth_sessions" (
  "signature" text NOT NULL,
  "client_id" text NULL,
  "subject" text NULL,
  "scopes" text NULL,
  "expires_at" timestamptz NULL,
  PRIMARY KEY ("signature")
);
-- reverse: drop "instance_invites" table
CREATE TABLE "instance_invites" (
  "token" text NOT NULL,
  "created_at" timestamptz NULL,
  "used_at" timestamptz NULL,
  PRIMARY KEY ("token")
);
