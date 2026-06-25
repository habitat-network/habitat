import * as Y from "yjs";
import type { PearClient } from "./pearClient";
import { renderDoc } from "./render";

const CRDT_COLLECTION = "network.habitat.docs.crdt";
const MARKDOWN_COLLECTION = "network.habitat.docs.markdown";
const SELF = "self";

export interface DocView {
  docId: string;
  uri: string;
  title: string;
}

// DocStore keeps each document's canonical Yjs state in memory so updates don't
// refetch from pear on every edit. Each document is its own space; the store
// writes a CRDT record and a rendered-markdown record, both keyed "self".
export class DocStore {
  private pear: PearClient;
  private docs = new Map<string, Y.Doc>();

  constructor(pear: PearClient) {
    this.pear = pear;
  }

  // createDoc creates a new doc space, seeds its records, and grants the
  // creating member write access so they can read it back directly.
  async createDoc(
    memberDid: string,
  ): Promise<{ uri: string; docId: string }> {
    const space = await this.pear.createSpace();
    const ydoc = new Y.Doc();
    await this.writeRecords(space.uri, ydoc);
    await this.pear.addMember(space.uri, memberDid, "write");
    this.docs.set(space.skey, ydoc);
    return { uri: space.uri, docId: space.skey };
  }

  // applyUpdate merges a Yjs update into a doc and rewrites both records.
  async applyUpdate(
    docId: string,
    updateB64: string,
    _: string, // memberDid - not doing attribution yet 
  ): Promise<{ uri: string; cid?: string }> {
    const orgDid = await this.pear.orgDid();
    const spaceUri = this.pear.spaceUri(docId, orgDid);
    const ydoc = await this.load(docId, spaceUri);
    Y.applyUpdateV2(ydoc, new Uint8Array(Buffer.from(updateB64, "base64")));
    const result = await this.writeRecords(spaceUri, ydoc);
    return result;
  }

  // listDocs returns every doc in the org, with titles read from each space's
  // markdown "self" record. Spaces without a markdown record (e.g. legacy) are
  // skipped.
  // TODO will be replaced by sap
  async listDocs(): Promise<DocView[]> {
    const spaces = await this.pear.listSpaces();
    const docs = await Promise.all(
      spaces.map(async (s): Promise<DocView | undefined> => {
        const md = await this.pear.getRecord(s.uri, MARKDOWN_COLLECTION, SELF);
        if (!md) {
          return undefined;
        }
        const title = (md.value as { title?: string }).title || "Untitled";
        return { docId: s.skey, uri: s.uri, title };
      }),
    );
    return docs.filter((d): d is DocView => d !== undefined);
  }

  private async load(docId: string, spaceUri: string): Promise<Y.Doc> {
    const cached = this.docs.get(docId);
    if (cached) {
      return cached;
    }
    const record = await this.pear.getRecord(spaceUri, CRDT_COLLECTION, SELF);
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
    spaceUri: string,
    ydoc: Y.Doc,
    nameOverride?: string,
  ): Promise<{ uri: string; cid?: string }> {
    const rendered = renderDoc(ydoc);
    const title = nameOverride ?? rendered.title;
    // TODO this should be a blob
    const crdt = await this.pear.putRecord(spaceUri, CRDT_COLLECTION, SELF, {
      blob: Buffer.from(Y.encodeStateAsUpdateV2(ydoc)).toString("base64"),
    });
    await this.pear.putRecord(spaceUri, MARKDOWN_COLLECTION, SELF, {
      title,
      content: rendered.markdown,
    });
    return { uri: crdt.uri, cid: crdt.cid };
  }
}
