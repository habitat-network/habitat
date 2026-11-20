import { PearApi, type Org } from "./api";

/**
 * Baseline data every run starts from. Add to it when several tests need the
 * same read-only fixture; tests that mutate state should create their own org
 * through the `createOrg` fixture instead.
 *
 * Why seed through the API rather than commit a database snapshot: the schema
 * is managed by goose migrations (cmd/pear/migrations), so a checked-in .db is
 * a binary blob that silently drifts from them, and the pear image runs on
 * Postgres anyway, not SQLite. Replaying the app's own endpoints on a freshly migrated database
 * stays correct as the schema evolves and is diffable in review.
 *
 * The same goes for the PDS accounts (harness/pds.ts). A snapshot of them
 * would be the PDS's data directory plus the PLC directory's database, kept
 * in step with each other, and the alpha PDS warns that its schema changes
 * without clean migrations, so a snapshot breaks whenever its image moves.
 * Creating the accounts takes about a second, against tens of seconds for
 * starting the test bed, so they are seeded idempotently instead.
 */
export interface SeedData {
  org: Org;
}

export async function seed(api: PearApi): Promise<SeedData> {
  const org = await api.createOrg({ handle: "seed", domain: "seed.test" });
  return { org };
}
