import { and, asc, eq, lte } from "drizzle-orm";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import type {
  PromiseSyncStore,
  RepoState,
  SpaceState,
} from "internal/spaceSync";
import type { Db } from "./index";
import { syncRepos, syncSpaces } from "./schema";

// DrizzleSyncStore is internal/spaceSync's durable SyncStore port over D1.
// Each method is a single statement (or a D1 batch, which D1 runs as one
// transaction), which is the atomicity the port asks for.

type SpaceRow = typeof syncSpaces.$inferSelect;

function toSpaceState(row: SpaceRow): SpaceState {
  return {
    space: row.space as SpaceRefString,
    authority: row.authority as DidString,
    spaceRev: row.spaceRev ?? undefined,
    registrationExpiresAt: row.registrationExpiresAt ?? undefined,
    nextDueAt: row.nextDueAt,
    lastFullPassAt: row.lastFullPassAt ?? undefined,
    failures: row.failures,
    lastError: row.lastError ?? undefined,
  };
}

function toSpaceRow(state: SpaceState): SpaceRow {
  return {
    space: state.space,
    authority: state.authority,
    spaceRev: state.spaceRev ?? null,
    registrationExpiresAt: state.registrationExpiresAt ?? null,
    nextDueAt: state.nextDueAt,
    lastFullPassAt: state.lastFullPassAt ?? null,
    failures: state.failures,
    lastError: state.lastError ?? null,
  };
}

export class DrizzleSyncStore implements PromiseSyncStore {
  constructor(private db: Db) {}

  async getSpace(space: SpaceRefString): Promise<SpaceState | undefined> {
    const [row] = await this.db
      .select()
      .from(syncSpaces)
      .where(eq(syncSpaces.space, space))
      .limit(1);
    return row ? toSpaceState(row) : undefined;
  }

  async putSpace(state: SpaceState): Promise<void> {
    const row = toSpaceRow(state);
    const { space: _, ...set } = row;
    await this.db
      .insert(syncSpaces)
      .values(row)
      .onConflictDoUpdate({ target: syncSpaces.space, set });
  }

  async removeSpace(space: SpaceRefString): Promise<void> {
    await this.db.batch([
      this.db.delete(syncRepos).where(eq(syncRepos.space, space)),
      this.db.delete(syncSpaces).where(eq(syncSpaces.space, space)),
    ]);
  }

  // A compare-and-set in one UPDATE: the WHERE clause only matches while
  // next_due_at still holds the value the caller saw, so of two racing
  // claimers exactly one changes a row.
  async claimDue(
    space: SpaceRefString,
    seen: number,
    until: number,
  ): Promise<boolean> {
    const updated = await this.db
      .update(syncSpaces)
      .set({ nextDueAt: until })
      .where(and(eq(syncSpaces.space, space), eq(syncSpaces.nextDueAt, seen)))
      .returning({ space: syncSpaces.space });
    return updated.length > 0;
  }

  async dueSpaces(
    now: number,
    limit: number,
  ): Promise<ReadonlyArray<SpaceState>> {
    const rows = await this.db
      .select()
      .from(syncSpaces)
      .where(lte(syncSpaces.nextDueAt, now))
      .orderBy(asc(syncSpaces.nextDueAt))
      .limit(limit);
    return rows.map(toSpaceState);
  }

  async getRepo(
    space: SpaceRefString,
    did: DidString,
  ): Promise<RepoState | undefined> {
    const [row] = await this.db
      .select()
      .from(syncRepos)
      .where(and(eq(syncRepos.space, space), eq(syncRepos.did, did)))
      .limit(1);
    if (!row) return undefined;
    return {
      space: row.space as SpaceRefString,
      did: row.did as DidString,
      rev: row.rev,
      ltHash: row.ltHash,
    };
  }

  async listRepoDids(space: SpaceRefString): Promise<ReadonlyArray<DidString>> {
    const rows = await this.db
      .select({ did: syncRepos.did })
      .from(syncRepos)
      .where(eq(syncRepos.space, space));
    return rows.map((r) => r.did as DidString);
  }

  async putRepo(state: RepoState): Promise<void> {
    // Copied rather than stored by reference: the syncer keeps mutating its
    // own LtHash buffer after this call returns.
    const ltHash = state.ltHash.slice();
    await this.db
      .insert(syncRepos)
      .values({ space: state.space, did: state.did, rev: state.rev, ltHash })
      .onConflictDoUpdate({
        target: [syncRepos.space, syncRepos.did],
        set: { rev: state.rev, ltHash },
      });
  }

  async removeRepo(space: SpaceRefString, did: DidString): Promise<void> {
    await this.db
      .delete(syncRepos)
      .where(and(eq(syncRepos.space, space), eq(syncRepos.did, did)));
  }
}
