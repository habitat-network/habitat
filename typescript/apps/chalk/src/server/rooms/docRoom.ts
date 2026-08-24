import { DurableObject } from "cloudflare:workers";
import { drizzle, type DrizzleSqliteDODatabase } from "drizzle-orm/durable-sqlite";
import { eq } from "drizzle-orm";
import * as Y from "yjs";
import { docState } from "./docRoomSchema";

export interface DocIdentity {
  spaceUri: string;
  // Optional: a doc shared with this member may not be in this deployment's
  // D1 index, and the member-edit flush writes to the member's own repo and
  // never needs the owner. The owner-republish flush is skipped while this
  // is unknown, mirroring today's `republishCanonical`'s `if (!doc) return;`.
  ownerDid?: string;
}

export class DocRoom extends DurableObject<Env> {
  private db: DrizzleSqliteDODatabase;
  private ydoc = new Y.Doc();
  private id: DocIdentity | undefined;

  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env);
    this.db = drizzle(ctx.storage);
    ctx.blockConcurrencyWhile(async () => {
      this.db.run(`CREATE TABLE IF NOT EXISTS doc_state (
        id INTEGER PRIMARY KEY, space_uri TEXT NOT NULL,
        owner_did TEXT, state BLOB, updated_at INTEGER NOT NULL)`);
      const [row] = await this.db.select().from(docState).where(eq(docState.id, 0)).limit(1);
      if (!row) return;
      this.id = { spaceUri: row.spaceUri, ownerDid: row.ownerDid ?? undefined };
      if (row.state) Y.applyUpdateV2(this.ydoc, new Uint8Array(row.state));
    });
  }

  async identity(): Promise<DocIdentity> {
    if (!this.id) throw new Error("DocRoom has no identity yet");
    return this.id;
  }

  async snapshot(): Promise<Uint8Array> {
    return Y.encodeStateAsUpdateV2(this.ydoc);
  }

  async applyEdit(id: DocIdentity, _memberDid: string, update: Uint8Array): Promise<void> {
    await this.rememberIdentity(id);
    await this.mergeUpdate(update);
  }

  async applyRemote(id: DocIdentity, _cid: string): Promise<void> {
    await this.rememberIdentity(id);
    // Blob dereference lands in Task 7.
  }

  // rememberIdentity records spaceUri/ownerDid the first time any caller
  // supplies them, and never downgrades a known ownerDid back to undefined.
  private async rememberIdentity(id: DocIdentity): Promise<void> {
    const next: DocIdentity = {
      spaceUri: id.spaceUri,
      ownerDid: id.ownerDid ?? this.id?.ownerDid,
    };
    if (this.id?.spaceUri === next.spaceUri && this.id?.ownerDid === next.ownerDid) return;
    this.id = next;
    await this.persist();
  }

  // mergeUpdate applies a Yjs update to the in-memory doc and persists the
  // merged result. `this.ydoc` is loaded from storage in the constructor,
  // never a bare `new Y.Doc()` at merge time: `update` is an *incremental*
  // diff relative to the sender's state, so merging it into an empty doc
  // would reconstruct only that fragment and persisting it would destroy
  // everything that came before.
  protected async mergeUpdate(update: Uint8Array): Promise<void> {
    Y.applyUpdateV2(this.ydoc, update);
    await this.persist();
  }

  private async persist(): Promise<void> {
    if (!this.id) return;
    const row = {
      id: 0,
      spaceUri: this.id.spaceUri,
      ownerDid: this.id.ownerDid ?? null,
      state: Buffer.from(Y.encodeStateAsUpdateV2(this.ydoc)),
      updatedAt: Date.now(),
    };
    await this.db.insert(docState).values(row).onConflictDoUpdate({ target: docState.id, set: row });
  }
}
