import * as Y from "yjs";
import type { PearClient } from "./pearClient";

interface CachedDoc {
  ydoc: Y.Doc;
  name: string;
}

interface DocRecordValue {
  name: string;
  blob: string;
  editorClique?: string;
}

// DocStore keeps the canonical Yjs document for each doc in memory so updates
// don't require refetching from pear on every edit. It is the only component
// that mutates the canonical record (via PearClient using the org credential).
export class DocStore {
  private pear: PearClient;
  private docs = new Map<string, CachedDoc>();

  constructor(pear: PearClient) {
    this.pear = pear;
  }

  // createDoc creates a new, empty document and grants the creating member
  // write access to the docs space so they can read it back directly.
  async createDoc(
    name: string,
    memberDid: string,
  ): Promise<{ uri: string; docId: string }> {
    const ydoc = new Y.Doc();
    const result = await this.pear.putRecord(encodeRecord(name, ydoc));
    const docId = result.uri.split("/").pop();
    if (!docId) {
      throw new Error(`unexpected record URI: ${result.uri}`);
    }
    this.docs.set(docId, { ydoc, name });
    await this.pear.addMember(memberDid, "write");
    return { uri: result.uri, docId };
  }

  // applyUpdate merges a Yjs update into the canonical document and writes it
  // back. The member is granted access in case they were not the creator.
  async applyUpdate(
    docId: string,
    updateB64: string,
    memberDid: string,
  ): Promise<{ uri: string; cid?: string }> {
    const cached = await this.load(docId);
    Y.applyUpdateV2(
      cached.ydoc,
      new Uint8Array(Buffer.from(updateB64, "base64")),
    );
    const result = await this.pear.putRecord(
      encodeRecord(cached.name, cached.ydoc),
      docId,
    );
    await this.pear.addMember(memberDid, "write");
    return { uri: result.uri, cid: result.cid };
  }

  private async load(docId: string): Promise<CachedDoc> {
    const cached = this.docs.get(docId);
    if (cached) {
      return cached;
    }
    const record = await this.pear.getRecord(docId);
    if (!record) {
      throw new Error(`doc not found: ${docId}`);
    }
    const value = record.value as unknown as DocRecordValue;
    const ydoc = new Y.Doc();
    if (value.blob) {
      Y.applyUpdateV2(ydoc, new Uint8Array(Buffer.from(value.blob, "base64")));
    }
    const hydrated: CachedDoc = { ydoc, name: value.name ?? "Untitled" };
    this.docs.set(docId, hydrated);
    return hydrated;
  }
}

function encodeRecord(name: string, ydoc: Y.Doc): Record<string, unknown> {
  return {
    name,
    blob: Buffer.from(Y.encodeStateAsUpdateV2(ydoc)).toString("base64"),
  };
}
