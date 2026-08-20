import { desc, eq, inArray } from "drizzle-orm";
import type { BetterSQLite3Database } from "drizzle-orm/better-sqlite3";
import * as Y from "yjs";
import * as schema from "../db/schema";

export interface DocSummary {
  docId: string;
  uri: string;
  ownerDid: string;
  title: string;
}

// DocStore persists the docs chalk has discovered (via its own creates or
// the sap-outbox consumer) and each doc's merged CRDT snapshot, in sqlite so
// both survive restarts.
export class DocStore {
  constructor(private db: BetterSQLite3Database<typeof schema>) {}

  upsertDoc(doc: {
    spaceUri: string;
    docId: string;
    ownerDid: string;
    title: string;
  }): void {
    this.db
      .insert(schema.docs)
      .values({
        spaceUri: doc.spaceUri,
        docId: doc.docId,
        ownerDid: doc.ownerDid,
        title: doc.title,
        updatedAt: Date.now(),
      })
      .onConflictDoUpdate({
        target: schema.docs.spaceUri,
        set: {
          docId: doc.docId,
          ownerDid: doc.ownerDid,
          title: doc.title,
          updatedAt: Date.now(),
        },
      })
      .run();
  }

  docsForOwner(ownerDid: string): DocSummary[] {
    return this.selectSummaries()
      .where(eq(schema.docs.ownerDid, ownerDid))
      .orderBy(desc(schema.docs.updatedAt))
      .all();
  }

  docsByUris(spaceUris: string[]): DocSummary[] {
    if (spaceUris.length === 0) return [];
    return this.selectSummaries()
      .where(inArray(schema.docs.spaceUri, spaceUris))
      .orderBy(desc(schema.docs.updatedAt))
      .all();
  }

  private selectSummaries() {
    return this.db
      .select({
        docId: schema.docs.docId,
        uri: schema.docs.spaceUri,
        ownerDid: schema.docs.ownerDid,
        title: schema.docs.title,
      })
      .from(schema.docs);
  }

  mergedState(spaceUri: string): Y.Doc | undefined {
    const row = this.db
      .select({ state: schema.docCrdt.state })
      .from(schema.docCrdt)
      .where(eq(schema.docCrdt.spaceUri, spaceUri))
      .get();
    if (!row) return undefined;
    const ydoc = new Y.Doc();
    Y.applyUpdateV2(ydoc, Buffer.from(row.state, "base64"));
    return ydoc;
  }

  persistMerged(spaceUri: string, ydoc: Y.Doc): void {
    const state = Buffer.from(Y.encodeStateAsUpdateV2(ydoc)).toString("base64");
    this.db
      .insert(schema.docCrdt)
      .values({ spaceUri, state, updatedAt: Date.now() })
      .onConflictDoUpdate({
        target: schema.docCrdt.spaceUri,
        set: { state, updatedAt: Date.now() },
      })
      .run();
  }
}
