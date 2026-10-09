-- +goose Up
-- create "workos_org_mappings" table
CREATE TABLE "workos_org_mappings" (
  "workos_org_id" text NOT NULL,
  "org_did" text NOT NULL,
  PRIMARY KEY ("workos_org_id")
);

-- +goose Down
-- reverse: create "workos_org_mappings" table
DROP TABLE "workos_org_mappings";
