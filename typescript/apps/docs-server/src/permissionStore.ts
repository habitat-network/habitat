import fs from "node:fs/promises";
import path from "node:path";

// PermissionStore persists, per doc, the flattened set of user DIDs that can
// read it (as discovered by crawling pear's relationship graph via
// listSubjects). listDocs references it to return only the docs a caller is
// actually permitted to see, without re-querying pear on every list.
//
// The snapshot is written to <dataDir>/permissions.json so it survives
// restarts; the crawler rewrites it wholesale on each pass.
export class PermissionStore {
  private filePath: string;
  // docId -> set of reader DIDs
  private readers = new Map<string, Set<string>>();

  constructor(dataDir: string) {
    this.filePath = path.join(dataDir, "permissions.json");
  }

  // load reads the persisted snapshot, if any. A missing or unreadable file is
  // treated as an empty store — the next crawl will repopulate it.
  async load(): Promise<void> {
    try {
      const raw = await fs.readFile(this.filePath, "utf8");
      const parsed = JSON.parse(raw) as Record<string, string[]>;
      this.readers = new Map(
        Object.entries(parsed).map(([docId, dids]) => [docId, new Set(dids)]),
      );
    } catch {
      this.readers = new Map();
    }
  }

  // replace swaps in a freshly-crawled snapshot and persists it.
  async replace(snapshot: Map<string, string[]>): Promise<void> {
    this.readers = new Map(
      [...snapshot].map(([docId, dids]) => [docId, new Set(dids)]),
    );
    await this.persist();
  }

  // canRead reports whether did is among the readers recorded for docId.
  canRead(docId: string, did: string): boolean {
    return this.readers.get(docId)?.has(did) ?? false;
  }

  private async persist(): Promise<void> {
    const obj: Record<string, string[]> = {};
    for (const [docId, dids] of this.readers) {
      obj[docId] = [...dids];
    }
    await fs.mkdir(path.dirname(this.filePath), { recursive: true });
    await fs.writeFile(this.filePath, JSON.stringify(obj), "utf8");
  }
}
