-- +goose Up
-- drop "blobs" table
DROP TABLE "blobs";
-- drop "clique_members" table
DROP TABLE "clique_members";
-- drop "links" table
DROP TABLE "links";
-- drop "members" table
DROP TABLE "members";
-- drop "password_entries" table
DROP TABLE "password_entries";
-- drop "permissions" table
DROP TABLE "permissions";
-- drop "records" table
DROP TABLE "records";
-- drop "organizations" table
DROP TABLE "organizations";

-- +goose Down
-- reverse: drop "organizations" table
CREATE TABLE "organizations" (
  "id" text NOT NULL,
  "name" text NULL,
  "login_method" text NULL,
  "signing_secret" text NULL,
  "created_at" timestamptz NULL,
  "handle_subdomain" text NULL,
  "contact_email" text NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "uni_organizations_handle_subdomain" UNIQUE ("handle_subdomain")
);
-- reverse: drop "records" table
CREATE TABLE "records" (
  "did" text NOT NULL,
  "collection" text NOT NULL,
  "rkey" text NOT NULL,
  "value" bytea NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("did", "collection", "rkey")
);
-- reverse: drop "permissions" table
CREATE TABLE "permissions" (
  "grantee" text NOT NULL,
  "owner" text NOT NULL,
  "collection" text NOT NULL,
  "rkey" text NOT NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("grantee", "owner", "collection", "rkey")
);
-- reverse: drop "password_entries" table
CREATE TABLE "password_entries" (
  "did" text NOT NULL,
  "password" text NULL,
  PRIMARY KEY ("did")
);
-- reverse: drop "members" table
CREATE TABLE "members" (
  "org_id" text NOT NULL,
  "did" text NOT NULL,
  "role" text NOT NULL,
  "login_id" text NOT NULL,
  "created_at" timestamptz NULL,
  PRIMARY KEY ("org_id", "did"),
  CONSTRAINT "fk_members_organization" FOREIGN KEY ("org_id") REFERENCES "organizations" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
CREATE INDEX "idx_members_login_id" ON "members" ("login_id");
-- reverse: drop "links" table
CREATE TABLE "links" (
  "cid" text NOT NULL,
  "did" text NOT NULL,
  "ref" text NOT NULL,
  PRIMARY KEY ("cid", "did", "ref")
);
-- reverse: drop "clique_members" table
CREATE TABLE "clique_members" (
  "owner" text NOT NULL,
  "key" text NOT NULL,
  "member" text NOT NULL,
  "created_at" timestamptz NULL,
  PRIMARY KEY ("owner", "key", "member")
);
-- reverse: drop "blobs" table
CREATE TABLE "blobs" (
  "id" bigserial NOT NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  "did" text NULL,
  "cid" text NULL,
  "mime_type" text NULL,
  "blob" bytea NULL,
  PRIMARY KEY ("id")
);
CREATE INDEX "idx_blobs_deleted_at" ON "blobs" ("deleted_at");
