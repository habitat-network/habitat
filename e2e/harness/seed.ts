import { PearApi, type Org } from "./api";

/**
 * Baseline data every run starts from. Add to it when several tests need the
 * same read-only fixture; tests that mutate state should create their own org
 * through the `createOrg` fixture instead.
 *
 * Why seed through the API rather than commit a SQLite file: the schema is
 * managed by goose migrations (cmd/pear/migrations), so a checked-in .db is a
 * binary blob that silently drifts from them (and from the Postgres schema
 * prod uses). Replaying the app's own endpoints on a freshly migrated database
 * stays correct as the schema evolves and is diffable in review.
 */
export interface SeedData {
  org: Org;
}

export async function seed(api: PearApi): Promise<SeedData> {
  const org = await api.createOrg({
    subdomain: "seed",
    name: "Seed Org",
    adminPassword: "seed-admin-password",
  });
  return { org };
}
