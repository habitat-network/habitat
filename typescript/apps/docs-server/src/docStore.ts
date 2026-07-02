import * as Y from "yjs";
import type { PearClient } from "./pearClient";
import { renderDoc } from "./render";

const CRDT_COLLECTION = "network.habitat.docs.crdt";
const MARKDOWN_COLLECTION = "network.habitat.docs.markdown";
const SELF = "self";

// DocStore keeps each document's canonical Yjs state in memory so updates don't
// refetch from pear on every edit. Each document is its own space, owned by the
// caller's org; the store writes a CRDT record and a rendered-markdown record,
// both keyed "self". The org DID is passed per call (from the caller's service
// auth) so one docs server can serve many orgs.
export class DocStore {
  private pear: PearClient;
  private docs = new Map<string, Y.Doc>();

  constructor(pear: PearClient) {
    this.pear = pear;
  }

  // createDoc creates a new doc space owned by the caller's org, seeds its
  // records, and grants the creating member write access so they can read it
  // back directly.
  async createDoc(
    memberDid: string,
    org: string,
  ): Promise<{ uri: string; docId: string }> {
    const space = await this.pear.createSpace(org);
    const ydoc = new Y.Doc();
    await this.writeRecords(org, space.uri, ydoc);
    await this.pear.addMember(org, space.uri, memberDid, "write");
    this.docs.set(space.skey, ydoc);
    return { uri: space.uri, docId: space.skey };
  }

  // applyUpdate merges a Yjs update into a doc and rewrites both records.
  async applyUpdate(
    docId: string,
    updateB64: string,
    _: string, // memberDid - not doing attribution yet
    org: string,
  ): Promise<{ uri: string; cid?: string }> {
    const spaceUri = this.pear.spaceUri(docId, org);
    const ydoc = await this.load(org, docId, spaceUri);
    Y.applyUpdateV2(ydoc, new Uint8Array(Buffer.from(updateB64, "base64")));
    const result = await this.writeRecords(org, spaceUri, ydoc);
    return result;
  }

  private async load(
    org: string,
    docId: string,
    spaceUri: string,
  ): Promise<Y.Doc> {
    const cached = this.docs.get(docId);
    if (cached) {
      return cached;
    }
    const record = await this.pear.getRecord(
      org,
      spaceUri,
      CRDT_COLLECTION,
      SELF,
    );
    const ydoc = new Y.Doc();
    const blob = record && (record.value as { blob?: string }).blob;
    if (blob) {
      Y.applyUpdateV2(ydoc, new Uint8Array(Buffer.from(blob, "base64")));
    }
    this.docs.set(docId, ydoc);
    return ydoc;
  }

  // writeRecords persists the CRDT state and a freshly-rendered markdown record.
  // nameOverride sets the title on creation (when the doc is still empty);
  // otherwise the title is derived from the rendered content.
  private async writeRecords(
    org: string,
    spaceUri: string,
    ydoc: Y.Doc,
    nameOverride?: string,
  ): Promise<{ uri: string; cid?: string }> {
    const rendered = renderDoc(ydoc);
    const title = nameOverride ?? rendered.title;
    // TODO this should be a blob
    const crdt = await this.pear.putRecord(
      org,
      spaceUri,
      CRDT_COLLECTION,
      SELF,
      {
        blob: Buffer.from(Y.encodeStateAsUpdateV2(ydoc)).toString("base64"),
      },
    );
    await this.pear.putRecord(org, spaceUri, MARKDOWN_COLLECTION, SELF, {
      title,
      content: rendered.markdown,
    });
    return { uri: crdt.uri, cid: crdt.cid };
  }
}
