# Document Comments Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let members add anchored comments (and one level of replies) to collaborative documents in `docsv2`, stored as per-user AT Protocol records in a **separate comment space** whose permissions are derived from the doc's space, indexed by `docs-server`, and exposed through a new `listComments` endpoint.

**Architecture:** Each document already lives in its own space (type `network.habitat.docs`, skey = `docId`). We give each doc a **companion comment space** of type `network.habitat.docs.comments` that **reuses the same skey**, so its URI is derivable from the doc id (`ats://<org>/network.habitat.docs.comments/<docId>`). At doc-creation time the docs-server (proxying as the org) also writes two **`spaceRoleSubject` userset tuples**: doc-space readers become comment-space readers, and doc-space writers become comment-space writers. This lets us grant someone comment-only access later by writing a plain writer tuple on the comment space without giving them the doc. Comments are `network.habitat.docs.comment` records written by each member into their own repo within the comment space via `network.habitat.space.putRecord` (member's own session), each carrying the related doc-space URI. A comment anchors to a text range using two **Yjs relative positions** (serialized to JSON) computed from the Tiptap/ProseMirror selection via `@tiptap/y-tiptap`. The sap crawler indexes comment records into a SQLite-backed `DocCommentStore` keyed by doc-space URI; the new `network.habitat.docs.listComments` query (proxied through pear like `listDocs`) authorizes the caller with `relationship.check` against the comment space and returns top-level comments each with their replies. The frontend gets the comment-space URI back in each doc's metadata and renders a sidebar, a selection-triggered composer, and highlight decorations.

**Tech Stack:** TypeScript, Yjs (`yjs` + `@tiptap/y-tiptap`), Tiptap 3 / ProseMirror, Hono, `node:sqlite` (`DatabaseSync`), Vitest, TanStack Query/Router, AT Protocol lexicons (codegen via `moon :generate`), Habitat rebac (`network.habitat.relationship.*`).

## Global Constraints

- **Separate comment space, deterministic skey.** The comment space is `ats://<org>/network.habitat.docs.comments/<docId>` — same skey as the doc space, different type. Never mint a random skey for it; always create it with the doc space's skey so the URI is derivable.
- **Derived permissions via userset tuples.** Comment-space access is granted through `spaceRoleSubject` tuples (`doc readers → comment readers`, `doc writers → comment writers`), *not* by copying members. Comment-only access = a direct writer tuple on the comment space.
- **Per-user repo.** Comment records are written into the *authoring member's* repo within the comment space, via `space.putRecord` with the member's own session (no docs-server proxy).
- **Every comment record carries `docSpace`** — the URI of the doc space it relates to (required field).
- **Anchoring uses Yjs relative positions via `@tiptap/y-tiptap`.** Never anchor on raw character indices — ProseMirror positions differ from Yjs `Y.Text` indices. Always go through `absolutePositionToRelativePosition` / `relativePositionToAbsolutePosition` with the `ySyncPluginKey` binding mapping.
- **Threading is two levels only** — top-level comments and their direct replies.
- **Never hand-edit generated code.** Files under `typescript/api/`, `api/habitat/`, and `*.gen.ts` are produced by `moon :generate`.
- **Crawler-driven, SQLite-persisted indexing** (mirrors `DocMetadataStore` / `DocCrdtStore`); no on-demand `listRecords` fan-out.
- Verbatim NSIDs: doc-space type = `network.habitat.docs`; comment-space type = `network.habitat.docs.comments`; comment record collection = `network.habitat.docs.comment`; endpoint = `network.habitat.docs.listComments`. Comment record key strategy = `tid` (server-assigned; `rkey` omitted on `putRecord`).
- Role implications (relied upon): `owner ⊇ manager ⊇ writer ⊇ reader`. The doc creator (owner of the doc space) is therefore a writer of it, so the writer userset grants them comment-write automatically.

---

## File Structure

**Lexicons (source of truth; drive codegen):**
- Create `lexicons/network/habitat/docs/comment.json` — the comment record type (with `docSpace`).
- Create `lexicons/network/habitat/docs/listComments.json` — the query + view defs.
- Modify `lexicons/network/habitat/docs/listDocs.json` — add `commentSpace` to `#docView`.

**docs-server (`typescript/apps/docs-server/`):**
- Modify `src/pearClient.ts` — parameterize `createSpace` (type + optional skey); add `commentSpaceUri`, `writeUsersetTuple`, `check`; export the type constants.
- Modify `src/docCrdtStore.ts` — `createDoc` creates the comment space + writes the two userset tuples.
- Create `src/docCommentStore.ts` — SQLite store keyed by doc-space URI.
- Modify `src/crawler.ts` — parse authoring repo/rkey; index comment-collection records under their derived doc space.
- Modify `src/server.ts` — accept the comment store + injectable verifier; add `commentSpace` to `listDocs`; add the `listComments` route (authorized via `relationship.check`).
- Modify `src/index.ts` — construct + wire `DocCommentStore`.
- Modify `package.json` / `moon.yml` — Vitest dev dep, `test` script + task.
- Create tests: `src/docCommentStore.test.ts`, `src/crawler.test.ts`, `src/docCrdtStore.test.ts`, `src/server.test.ts`.

**Shared client (`typescript/internal/`):**
- Modify `src/habitatClient.ts` — register `network.habitat.docs.listComments`.

**Frontend (`typescript/apps/docsv2/`):**
- Modify `src/queries/docs.tsx` — surface `commentSpace` on `DocSummary`.
- Create `src/lib/anchor.ts` (+ test) — relative-position (de)serialization + selection↔range conversion.
- Create `src/queries/comments.tsx` — `listComments` query + `createComment`/`createReply` (write to the comment space, include `docSpace`).
- Create `src/extensions/commentHighlight.ts` — decoration extension.
- Create `src/components/CommentsSidebar.tsx` (+ test).
- Modify `src/routes/_requireAuth/$uri.tsx` — wire composer, sidebar, highlights.
- Modify `src/index.css` — highlight style.

---

## Task 1: Lexicons — comment record, listComments, docView.commentSpace

**Files:**
- Create: `lexicons/network/habitat/docs/comment.json`
- Create: `lexicons/network/habitat/docs/listComments.json`
- Modify: `lexicons/network/habitat/docs/listDocs.json`

**Interfaces:**
- Produces (generated into `api` by `moon :generate`):
  - `NetworkHabitatDocsComment.Record` — `{ body: string; createdAt: string; docSpace: string; range?: { start: string; end: string }; parent?: string }`
  - `NetworkHabitatDocsListComments.QueryParams` — `{ docId: string }`
  - `NetworkHabitatDocsListComments.OutputSchema` — `{ comments: CommentView[] }`
  - `NetworkHabitatDocsListComments.CommentView` — `{ uri: string; author: string; body: string; createdAt: string; range?: { start: string; end: string }; replies: ReplyView[] }`
  - `NetworkHabitatDocsListComments.ReplyView` — `{ uri: string; author: string; body: string; createdAt: string }`
  - `NetworkHabitatDocsListDocs.DocView` gains optional `commentSpace: string`.

- [ ] **Step 1: Write the comment record lexicon**

Create `lexicons/network/habitat/docs/comment.json`:

```json
{
    "lexicon": 1,
    "id": "network.habitat.docs.comment",
    "defs": {
        "main": {
            "type": "record",
            "description": "A comment on a collaborative document, written by the commenting member into the document's companion comment space. Top-level comments anchor to a text range via Yjs relative positions; replies reference a parent comment and omit the range.",
            "key": "tid",
            "record": {
                "type": "object",
                "required": [
                    "body",
                    "createdAt",
                    "docSpace"
                ],
                "properties": {
                    "body": {
                        "type": "string",
                        "maxLength": 10000,
                        "maxGraphemes": 3000,
                        "description": "The comment text."
                    },
                    "createdAt": {
                        "type": "string",
                        "format": "datetime",
                        "description": "When the comment was authored."
                    },
                    "docSpace": {
                        "type": "string",
                        "format": "uri",
                        "description": "URI of the document space this comment relates to."
                    },
                    "range": {
                        "type": "ref",
                        "ref": "#range",
                        "description": "The anchored text range. Present on top-level comments; omitted on replies."
                    },
                    "parent": {
                        "type": "string",
                        "description": "Space-record URI of the parent comment this replies to. Omitted for top-level comments."
                    }
                }
            }
        },
        "range": {
            "type": "object",
            "required": [
                "start",
                "end"
            ],
            "properties": {
                "start": {
                    "type": "string",
                    "description": "JSON-encoded Yjs relative position of the range start."
                },
                "end": {
                    "type": "string",
                    "description": "JSON-encoded Yjs relative position of the range end."
                }
            }
        }
    }
}
```

- [ ] **Step 2: Write the listComments query lexicon**

Create `lexicons/network/habitat/docs/listComments.json`:

```json
{
    "lexicon": 1,
    "id": "network.habitat.docs.listComments",
    "defs": {
        "main": {
            "type": "query",
            "description": "List the comment threads on a document. Implemented by the docs server, which authorizes the caller against the document's comment space and groups crawled comment records into threads.",
            "parameters": {
                "type": "params",
                "required": [
                    "docId"
                ],
                "properties": {
                    "docId": {
                        "type": "string",
                        "description": "The document's space key."
                    }
                }
            },
            "output": {
                "encoding": "application/json",
                "schema": {
                    "type": "object",
                    "required": [
                        "comments"
                    ],
                    "properties": {
                        "comments": {
                            "type": "array",
                            "items": {
                                "type": "ref",
                                "ref": "#commentView"
                            }
                        }
                    }
                }
            }
        },
        "commentView": {
            "type": "object",
            "required": [
                "uri",
                "author",
                "body",
                "createdAt",
                "replies"
            ],
            "properties": {
                "uri": {
                    "type": "string",
                    "description": "The comment record's space-record URI; used as the parent reference when replying."
                },
                "author": {
                    "type": "string",
                    "format": "did",
                    "description": "DID of the comment's author."
                },
                "body": {
                    "type": "string"
                },
                "createdAt": {
                    "type": "string",
                    "format": "datetime"
                },
                "range": {
                    "type": "ref",
                    "ref": "#rangeView",
                    "description": "The anchored range for top-level comments."
                },
                "replies": {
                    "type": "array",
                    "items": {
                        "type": "ref",
                        "ref": "#replyView"
                    },
                    "description": "Direct replies to this comment, oldest first."
                }
            }
        },
        "replyView": {
            "type": "object",
            "required": [
                "uri",
                "author",
                "body",
                "createdAt"
            ],
            "properties": {
                "uri": {
                    "type": "string"
                },
                "author": {
                    "type": "string",
                    "format": "did"
                },
                "body": {
                    "type": "string"
                },
                "createdAt": {
                    "type": "string",
                    "format": "datetime"
                }
            }
        },
        "rangeView": {
            "type": "object",
            "required": [
                "start",
                "end"
            ],
            "properties": {
                "start": {
                    "type": "string"
                },
                "end": {
                    "type": "string"
                }
            }
        }
    }
}
```

- [ ] **Step 3: Add `commentSpace` to the docView**

In `lexicons/network/habitat/docs/listDocs.json`, add a property to `defs.docView.properties` (leave `required` as-is — it stays optional so docs still list even before the space is known to a caller):

```json
                "commentSpace": {
                    "type": "string",
                    "format": "uri",
                    "description": "URI of the document's companion comment space, where comment records are written."
                }
```

- [ ] **Step 4: Regenerate + verify + typecheck**

Run: `moon :generate`
Expected: completes without error.

Run: `ls typescript/api/src/client/types/network/habitat/docs/`
Expected: includes `comment.ts` and `listComments.ts`.

Run: `pnpm --filter api build`
Expected: builds clean.

- [ ] **Step 5: Commit**

```bash
git add lexicons/network/habitat/docs typescript/api api/habitat api-docs
git commit -m "feat(docs): comment record + listComments lexicons; docView.commentSpace"
```

---

## Task 2: DocCommentStore (keyed by doc space) + Vitest setup

**Files:**
- Modify: `typescript/apps/docs-server/package.json`, `typescript/apps/docs-server/moon.yml`
- Create: `typescript/apps/docs-server/src/docCommentStore.ts`
- Test: `typescript/apps/docs-server/src/docCommentStore.test.ts`

**Interfaces:**
- Produces (used by Tasks 4 & 5):
  - `interface CommentRange { start: string; end: string }`
  - `interface StoredComment { docSpace: string; uri: string; author: string; body: string; createdAt: string; parentUri?: string; range?: CommentRange }`
  - `interface ReplyView { uri: string; author: string; body: string; createdAt: string }`
  - `interface CommentView extends ReplyView { range?: CommentRange; replies: ReplyView[] }`
  - `class DocCommentStore { constructor(db: DatabaseSync); upsertComment(c: StoredComment): void; threadsForDoc(docSpace: string): CommentView[] }`

- [ ] **Step 1: Add Vitest to docs-server**

In `typescript/apps/docs-server/package.json`, add to `scripts`:

```json
    "test": "vitest run"
```

and to `devDependencies`:

```json
    "vitest": "catalog:"
```

- [ ] **Step 2: Add a `test` task to moon.yml**

Append under `tasks:` in `typescript/apps/docs-server/moon.yml`:

```yaml
  test:
    command: pnpm test
```

- [ ] **Step 3: Install**

Run: `pnpm install`
Expected: installs `vitest` into docs-server.

- [ ] **Step 4: Write the failing store test**

Create `typescript/apps/docs-server/src/docCommentStore.test.ts`:

```ts
import { DatabaseSync } from "node:sqlite";
import { describe, expect, it } from "vitest";
import { DocCommentStore } from "./docCommentStore";

const DOC = "ats://did:web:org.example/network.habitat.docs/doc1";
const CSPACE = "ats://did:web:org.example/network.habitat.docs.comments/doc1";

function newStore(): DocCommentStore {
  return new DocCommentStore(new DatabaseSync(":memory:"));
}

describe("DocCommentStore", () => {
  it("returns a top-level comment with its range and no replies", () => {
    const store = newStore();
    store.upsertComment({
      docSpace: DOC,
      uri: `${CSPACE}/did:web:alice/network.habitat.docs.comment/c1`,
      author: "did:web:alice",
      body: "first",
      createdAt: "2026-07-07T00:00:00.000Z",
      range: { start: "s", end: "e" },
    });

    const threads = store.threadsForDoc(DOC);

    expect(threads).toHaveLength(1);
    expect(threads[0].body).toBe("first");
    expect(threads[0].author).toBe("did:web:alice");
    expect(threads[0].range).toEqual({ start: "s", end: "e" });
    expect(threads[0].replies).toEqual([]);
  });

  it("nests replies under their parent, oldest first, excluded from top level", () => {
    const store = newStore();
    const parentUri = `${CSPACE}/did:web:alice/network.habitat.docs.comment/c1`;
    store.upsertComment({
      docSpace: DOC,
      uri: parentUri,
      author: "did:web:alice",
      body: "parent",
      createdAt: "2026-07-07T00:00:00.000Z",
      range: { start: "s", end: "e" },
    });
    store.upsertComment({
      docSpace: DOC,
      uri: `${CSPACE}/did:web:bob/network.habitat.docs.comment/r2`,
      author: "did:web:bob",
      body: "second reply",
      createdAt: "2026-07-07T00:00:02.000Z",
      parentUri,
    });
    store.upsertComment({
      docSpace: DOC,
      uri: `${CSPACE}/did:web:bob/network.habitat.docs.comment/r1`,
      author: "did:web:bob",
      body: "first reply",
      createdAt: "2026-07-07T00:00:01.000Z",
      parentUri,
    });

    const threads = store.threadsForDoc(DOC);

    expect(threads).toHaveLength(1);
    expect(threads[0].replies.map((r) => r.body)).toEqual([
      "first reply",
      "second reply",
    ]);
  });

  it("upsert on the same uri overwrites the previous body", () => {
    const store = newStore();
    const uri = `${CSPACE}/did:web:alice/network.habitat.docs.comment/c1`;
    const base = {
      docSpace: DOC,
      uri,
      author: "did:web:alice",
      createdAt: "2026-07-07T00:00:00.000Z",
      range: { start: "s", end: "e" },
    };
    store.upsertComment({ ...base, body: "before" });
    store.upsertComment({ ...base, body: "after" });

    const threads = store.threadsForDoc(DOC);
    expect(threads).toHaveLength(1);
    expect(threads[0].body).toBe("after");
  });

  it("orders top-level comments oldest first", () => {
    const store = newStore();
    store.upsertComment({
      docSpace: DOC,
      uri: `${CSPACE}/did:web:alice/network.habitat.docs.comment/b`,
      author: "did:web:alice",
      body: "newer",
      createdAt: "2026-07-07T00:00:05.000Z",
    });
    store.upsertComment({
      docSpace: DOC,
      uri: `${CSPACE}/did:web:alice/network.habitat.docs.comment/a`,
      author: "did:web:alice",
      body: "older",
      createdAt: "2026-07-07T00:00:01.000Z",
    });

    const threads = store.threadsForDoc(DOC);
    expect(threads.map((t) => t.body)).toEqual(["older", "newer"]);
  });
});
```

- [ ] **Step 5: Run the test to verify it fails**

Run: `pnpm --filter docs-server test`
Expected: FAIL — cannot resolve `./docCommentStore`.

- [ ] **Step 6: Implement the store**

Create `typescript/apps/docs-server/src/docCommentStore.ts`:

```ts
import type { DatabaseSync } from "node:sqlite";

export interface CommentRange {
  start: string;
  end: string;
}

// StoredComment is one comment record the crawler observed, keyed by its full
// space-record URI and grouped by the doc space it relates to. Top-level
// comments carry a range; replies carry a parentUri.
export interface StoredComment {
  docSpace: string;
  uri: string;
  author: string;
  body: string;
  createdAt: string;
  parentUri?: string;
  range?: CommentRange;
}

export interface ReplyView {
  uri: string;
  author: string;
  body: string;
  createdAt: string;
}

export interface CommentView extends ReplyView {
  range?: CommentRange;
  replies: ReplyView[];
}

// DocCommentStore persists the comment records the sap crawler discovers,
// grouped by the doc space they relate to. Comments live in each member's repo
// within the doc's companion comment space (written with the member's own
// session), so the store is the docs server's index across all members'
// comment records for a document. Backed by sqlite (shared with the other doc
// stores) so it survives restarts; sap only redelivers unacked messages.
export class DocCommentStore {
  private db: DatabaseSync;

  constructor(db: DatabaseSync) {
    this.db = db;
    this.db.exec(`
      CREATE TABLE IF NOT EXISTS doc_comments (
        uri         TEXT PRIMARY KEY,
        doc_space   TEXT NOT NULL,
        author      TEXT NOT NULL,
        body        TEXT NOT NULL,
        created_at  TEXT NOT NULL,
        parent_uri  TEXT,
        range_start TEXT,
        range_end   TEXT,
        updated_at  INTEGER NOT NULL
      );
      CREATE INDEX IF NOT EXISTS doc_comments_doc
        ON doc_comments (doc_space);
    `);
  }

  // upsertComment records (or overwrites) one comment keyed by its record URI.
  upsertComment(c: StoredComment): void {
    this.db
      .prepare(
        `INSERT INTO doc_comments
           (uri, doc_space, author, body, created_at, parent_uri, range_start, range_end, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
         ON CONFLICT(uri) DO UPDATE SET
           doc_space = excluded.doc_space,
           author = excluded.author,
           body = excluded.body,
           created_at = excluded.created_at,
           parent_uri = excluded.parent_uri,
           range_start = excluded.range_start,
           range_end = excluded.range_end,
           updated_at = excluded.updated_at`,
      )
      .run(
        c.uri,
        c.docSpace,
        c.author,
        c.body,
        c.createdAt,
        c.parentUri ?? null,
        c.range?.start ?? null,
        c.range?.end ?? null,
        Date.now(),
      );
  }

  // threadsForDoc returns the document's comment threads: top-level comments
  // (no parent) oldest first, each with its direct replies oldest first. A
  // reply whose parent isn't indexed is promoted to top level so no comment is
  // dropped; it re-nests on a later rebuild once the parent is indexed.
  threadsForDoc(docSpace: string): CommentView[] {
    const rows = this.db
      .prepare(
        `SELECT uri, author, body, created_at AS createdAt,
                parent_uri AS parentUri, range_start AS rangeStart, range_end AS rangeEnd
         FROM doc_comments
         WHERE doc_space = ?
         ORDER BY created_at ASC`,
      )
      .all(docSpace) as {
      uri: string;
      author: string;
      body: string;
      createdAt: string;
      parentUri: string | null;
      rangeStart: string | null;
      rangeEnd: string | null;
    }[];

    const byUri = new Map<string, CommentView>();
    const replies: { parentUri: string; reply: ReplyView }[] = [];

    for (const r of rows) {
      const base: ReplyView = {
        uri: r.uri,
        author: r.author,
        body: r.body,
        createdAt: r.createdAt,
      };
      if (r.parentUri !== null) {
        replies.push({ parentUri: r.parentUri, reply: base });
      } else {
        const view: CommentView = { ...base, replies: [] };
        if (r.rangeStart !== null && r.rangeEnd !== null) {
          view.range = { start: r.rangeStart, end: r.rangeEnd };
        }
        byUri.set(r.uri, view);
      }
    }

    const orphans: CommentView[] = [];
    for (const { parentUri, reply } of replies) {
      const parent = byUri.get(parentUri);
      if (parent) {
        parent.replies.push(reply);
      } else {
        orphans.push({ ...reply, replies: [] });
      }
    }

    // Rows came back oldest-first, so top-level order and each replies array
    // preserve that ordering.
    return [...byUri.values(), ...orphans];
  }
}
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `pnpm --filter docs-server test`
Expected: PASS — all four cases green.

- [ ] **Step 8: Commit**

```bash
git add typescript/apps/docs-server/package.json typescript/apps/docs-server/moon.yml typescript/apps/docs-server/src/docCommentStore.ts typescript/apps/docs-server/src/docCommentStore.test.ts pnpm-lock.yaml
git commit -m "feat(docs-server): DocCommentStore + vitest"
```

---

## Task 3: PearClient comment-space helpers + createDoc wiring

**Files:**
- Modify: `typescript/apps/docs-server/src/pearClient.ts`
- Modify: `typescript/apps/docs-server/src/docCrdtStore.ts`
- Test: `typescript/apps/docs-server/src/docCrdtStore.test.ts`

**Interfaces:**
- Produces:
  - Exported constants `DOCS_SPACE_TYPE = "network.habitat.docs"`, `COMMENT_SPACE_TYPE = "network.habitat.docs.comments"`.
  - `PearClient.createSpace(org: string, type: string, skey?: string): Promise<SpaceRef>` (was `createSpace(org)`).
  - `PearClient.commentSpaceUri(skey: string, org: string): string`
  - `PearClient.writeUsersetTuple(org: string, subjectSpace: string, subjectRole: Role, relation: Role, object: string): Promise<void>` where `type Role = "owner" | "manager" | "writer" | "reader"`.
  - `PearClient.check(org: string, did: string, relation: Role, space: string): Promise<boolean>`
  - `DocCrdtStore.createDoc` now also creates the comment space and writes the two userset tuples (same return shape `{ uri, docId }`).

- [ ] **Step 1: Write the failing createDoc test**

Create `typescript/apps/docs-server/src/docCrdtStore.test.ts`:

```ts
import { DatabaseSync } from "node:sqlite";
import { describe, expect, it, vi } from "vitest";
import { DocCrdtStore } from "./docCrdtStore";
import {
  DOCS_SPACE_TYPE,
  COMMENT_SPACE_TYPE,
  type PearClient,
} from "./pearClient";

const ORG = "did:web:org.example";
const DOC_ID = "doc1";
const DOC_SPACE = `ats://${ORG}/${DOCS_SPACE_TYPE}/${DOC_ID}`;
const COMMENT_SPACE = `ats://${ORG}/${COMMENT_SPACE_TYPE}/${DOC_ID}`;

function fakePear() {
  return {
    createSpace: vi
      .fn()
      .mockImplementationOnce(async () => ({ uri: DOC_SPACE, skey: DOC_ID }))
      .mockImplementationOnce(async () => ({
        uri: COMMENT_SPACE,
        skey: DOC_ID,
      })),
    putRecord: vi.fn().mockResolvedValue({ uri: `${DOC_SPACE}/x`, cid: "c" }),
    grantRole: vi.fn().mockResolvedValue(undefined),
    writeUsersetTuple: vi.fn().mockResolvedValue(undefined),
  } as unknown as PearClient & {
    createSpace: ReturnType<typeof vi.fn>;
    writeUsersetTuple: ReturnType<typeof vi.fn>;
    grantRole: ReturnType<typeof vi.fn>;
  };
}

describe("DocCrdtStore.createDoc", () => {
  it("creates the doc space, a comment space with the doc's skey, and derived-permission tuples", async () => {
    const pear = fakePear();
    const store = new DocCrdtStore(pear, new DatabaseSync(":memory:"));

    const result = await store.createDoc("did:web:alice", ORG);

    expect(result).toEqual({ uri: DOC_SPACE, docId: DOC_ID });

    // Doc space first (auto skey), then the comment space with the same skey.
    expect(pear.createSpace).toHaveBeenNthCalledWith(1, ORG, DOCS_SPACE_TYPE);
    expect(pear.createSpace).toHaveBeenNthCalledWith(
      2,
      ORG,
      COMMENT_SPACE_TYPE,
      DOC_ID,
    );

    // Userset tuples: doc readers -> comment readers, doc writers -> comment writers.
    expect(pear.writeUsersetTuple).toHaveBeenCalledWith(
      ORG,
      DOC_SPACE,
      "reader",
      "reader",
      COMMENT_SPACE,
    );
    expect(pear.writeUsersetTuple).toHaveBeenCalledWith(
      ORG,
      DOC_SPACE,
      "writer",
      "writer",
      COMMENT_SPACE,
    );

    // Creator still gets owner on the doc space.
    expect(pear.grantRole).toHaveBeenCalledWith(
      ORG,
      DOC_SPACE,
      "did:web:alice",
      "owner",
    );
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm --filter docs-server test docCrdtStore`
Expected: FAIL — `DOCS_SPACE_TYPE`/`COMMENT_SPACE_TYPE` aren't exported and `writeUsersetTuple`/the second `createSpace` call don't exist.

- [ ] **Step 3: Update PearClient — constants, createSpace, comment-space URI**

In `typescript/apps/docs-server/src/pearClient.ts`:

Add `NetworkHabitatRelationshipCheck` to the `from "api"` type import block.

Export the space-type constants and a role type, replacing the existing private `DOCS_SPACE_TYPE` line:

```ts
export type Role = "owner" | "manager" | "writer" | "reader";

// Space types. A document lives in a doc space; its companion comment space
// reuses the same skey under the comments type, so the URI is derivable.
export const DOCS_SPACE_TYPE = "network.habitat.docs";
export const COMMENT_SPACE_TYPE = "network.habitat.docs.comments";
```

Replace `createSpace` to take a `type` and optional `skey`:

```ts
  // createSpace creates a new space of the given type owned by the org. When
  // skey is omitted pear generates one; pass it to pin the key (used to give a
  // doc's comment space the same skey as the doc space).
  async createSpace(
    org: string,
    type: string,
    skey?: string,
  ): Promise<SpaceRef> {
    const created =
      await this.call<NetworkHabitatSpaceCreateSpace.OutputSchema>(
        org,
        "network.habitat.space.createSpace",
        "POST",
        { type, skey } satisfies NetworkHabitatSpaceCreateSpace.InputSchema,
      );
    return { uri: created.uri, skey: skeyOf(created.uri) };
  }
```

Add a comment-space URI helper next to `spaceUri`:

```ts
  // commentSpaceUri reconstructs a doc's companion comment space URI from the
  // doc's skey. Comment spaces reuse the doc skey under the comments type.
  commentSpaceUri(skey: string, orgDid: string): string {
    return `ats://${orgDid}/${COMMENT_SPACE_TYPE}/${skey}`;
  }
```

- [ ] **Step 4: Add writeUsersetTuple + check to PearClient**

Still in `pearClient.ts`, add these methods (near `grantRole`):

```ts
  // writeUsersetTuple grants a role on the object space to everyone holding
  // subjectRole on subjectSpace (a spaceRoleSubject userset), enabling
  // cross-space permission inheritance — e.g. a doc's writers become its
  // comment space's writers. sap proxies as the org (object-space owner), which
  // passes writeTuple's manager check.
  async writeUsersetTuple(
    org: string,
    subjectSpace: string,
    subjectRole: Role,
    relation: Role,
    object: string,
  ): Promise<void> {
    await this.call<NetworkHabitatRelationshipWriteTuple.OutputSchema>(
      org,
      "network.habitat.relationship.writeTuple",
      "POST",
      {
        subject: {
          $type: "network.habitat.relationship.defs#spaceRoleSubject",
          space: subjectSpace,
          role: subjectRole,
        },
        relation,
        object: { space: object },
      } satisfies NetworkHabitatRelationshipWriteTuple.InputSchema,
    );
  }

  // check resolves whether a user holds a role on a space (through role
  // implications and usersets). Used to authorize listComments against the
  // comment space. sap proxies as the org, which holds reader on the space.
  async check(
    org: string,
    did: string,
    relation: Role,
    space: string,
  ): Promise<boolean> {
    const out = await this.call<NetworkHabitatRelationshipCheck.OutputSchema>(
      org,
      "network.habitat.relationship.check",
      "GET",
      { subject: did, relation, space } satisfies NetworkHabitatRelationshipCheck.QueryParams,
    );
    return out.allowed;
  }
```

- [ ] **Step 5: Update DocCrdtStore.createDoc**

In `typescript/apps/docs-server/src/docCrdtStore.ts`, import the space-type constants (add to the existing imports):

```ts
import { DOCS_SPACE_TYPE, COMMENT_SPACE_TYPE } from "./pearClient";
```

Replace the body of `createDoc`:

```ts
  async createDoc(
    memberDid: string,
    org: string,
  ): Promise<{ uri: string; docId: string }> {
    const space = await this.pear.createSpace(org, DOCS_SPACE_TYPE);
    // Companion comment space: same skey as the doc, different type, so its URI
    // is derivable from the doc id everywhere.
    const commentSpace = await this.pear.createSpace(
      org,
      COMMENT_SPACE_TYPE,
      space.skey,
    );
    // Derive comment permissions from the doc: doc readers can read comments,
    // doc writers can write them. A comment-only grant is a direct writer tuple
    // on the comment space (not written here).
    await this.pear.writeUsersetTuple(
      org,
      space.uri,
      "reader",
      "reader",
      commentSpace.uri,
    );
    await this.pear.writeUsersetTuple(
      org,
      space.uri,
      "writer",
      "writer",
      commentSpace.uri,
    );
    const ydoc = new Y.Doc();
    await this.writeRecords(org, space.uri, ydoc);
    this.persist(space.uri, ydoc);
    await this.pear.grantRole(org, space.uri, memberDid, "owner");
    return { uri: space.uri, docId: space.skey };
  }
```

- [ ] **Step 6: Run the createDoc test to verify it passes**

Run: `pnpm --filter docs-server test docCrdtStore`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add typescript/apps/docs-server/src/pearClient.ts typescript/apps/docs-server/src/docCrdtStore.ts typescript/apps/docs-server/src/docCrdtStore.test.ts
git commit -m "feat(docs-server): create comment space + derived-permission tuples on doc create"
```

---

## Task 4: Index comment records in the crawler

**Files:**
- Modify: `typescript/apps/docs-server/src/crawler.ts`
- Test: `typescript/apps/docs-server/src/crawler.test.ts`

**Interfaces:**
- Consumes: `DocCommentStore` (Task 2).
- Produces:
  - `parseSpaceRecordUri` also returns `repo` (URI part 3, authoring member's DID) and `rkey` (part 5).
  - `Crawler` constructor takes a `DocCommentStore` as its final argument: `new Crawler(config, meta, crdt, orgs, comments)`.

- [ ] **Step 1: Write the failing parse test**

Create `typescript/apps/docs-server/src/crawler.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { parseSpaceRecordUri } from "./crawler";

describe("parseSpaceRecordUri", () => {
  it("extracts the authoring repo and rkey from a comment record URI", () => {
    const uri =
      "ats://did:web:org/network.habitat.docs.comments/doc1/did:web:alice/network.habitat.docs.comment/c1";
    const parsed = parseSpaceRecordUri(uri);
    expect(parsed).toEqual({
      spaceUri: "ats://did:web:org/network.habitat.docs.comments/doc1",
      owner: "did:web:org",
      type: "network.habitat.docs.comments",
      skey: "doc1",
      repo: "did:web:alice",
      collection: "network.habitat.docs.comment",
      rkey: "c1",
    });
  });

  it("returns undefined for a non-record URI", () => {
    expect(parseSpaceRecordUri("ats://did:web:org/type/skey")).toBeUndefined();
    expect(parseSpaceRecordUri("https://example.com")).toBeUndefined();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm --filter docs-server test crawler`
Expected: FAIL — parsed object lacks `repo` and `rkey`.

- [ ] **Step 3: Extend `parseSpaceRecordUri`**

In `typescript/apps/docs-server/src/crawler.ts`, replace the `ParsedRecordUri` interface:

```ts
interface ParsedRecordUri {
  spaceUri: string;
  owner: string;
  type: string;
  skey: string;
  repo: string;
  collection: string;
  rkey: string;
}
```

Replace the parser body (keep the `ats://` prefix + length-6 guards):

```ts
export function parseSpaceRecordUri(uri: string): ParsedRecordUri | undefined {
  if (!uri.startsWith("ats://")) {
    return undefined;
  }
  const parts = uri.slice("ats://".length).split("/");
  if (parts.length !== 6) {
    return undefined;
  }
  const [owner, type, skey, repo, collection, rkey] = parts;
  if (!owner || !type || !skey || !repo || !collection || !rkey) {
    return undefined;
  }
  return {
    spaceUri: `ats://${owner}/${type}/${skey}`,
    owner,
    type,
    skey,
    repo,
    collection,
    rkey,
  };
}
```

- [ ] **Step 4: Run the parse test to verify it passes**

Run: `pnpm --filter docs-server test crawler`
Expected: PASS.

- [ ] **Step 5: Add constants, store, and comment handling to the crawler**

In `crawler.ts`, add the import:

```ts
import type { DocCommentStore } from "./docCommentStore";
import { DOCS_SPACE_TYPE } from "./pearClient";
```

Add a constant next to `CRDT_COLLECTION`:

```ts
// Comment records live in a doc's companion comment space, authored by members
// into their own repo; the crawler indexes them under the doc space they name.
const COMMENT_COLLECTION = "network.habitat.docs.comment";
```

Extend the constructor (add `comments` last):

```ts
  constructor(
    private config: DerivedConfig,
    private meta: DocMetadataStore,
    private crdt: DocCrdtStore,
    private orgs: OrgDirectory,
    private comments: DocCommentStore,
  ) {}
```

In `process`, add a branch before the trailing "Some other collection" comment (after the `CRDT_COLLECTION` block):

```ts
    if (parsed.collection === COMMENT_COLLECTION) {
      const value = (msg.value ?? {}) as {
        body?: string;
        createdAt?: string;
        parent?: string;
        docSpace?: string;
        range?: { start: string; end: string };
      };
      if (!value.body || !value.createdAt) {
        return;
      }
      // The comment space reuses the doc's skey, so the related doc space is
      // derivable from the comment record's own space URI. We derive it (rather
      // than trusting the record's docSpace field) so a comment can't be
      // attributed to a doc whose comment space the author can't write.
      const docSpace = `ats://${parsed.owner}/${DOCS_SPACE_TYPE}/${parsed.skey}`;
      this.comments.upsertComment({
        docSpace,
        uri: msg.uri,
        author: parsed.repo,
        body: value.body,
        createdAt: value.createdAt,
        parentUri: value.parent,
        range: value.range,
      });
      return;
    }
```

- [ ] **Step 6: Verify the crawler tests still pass**

Run: `pnpm --filter docs-server test crawler`
Expected: PASS. (A full `build` will fail only at `index.ts`, fixed in Task 5 — don't gate on it here.)

- [ ] **Step 7: Commit**

```bash
git add typescript/apps/docs-server/src/crawler.ts typescript/apps/docs-server/src/crawler.test.ts
git commit -m "feat(docs-server): index comment records in the crawler"
```

---

## Task 5: listComments endpoint + docView.commentSpace + wiring

**Files:**
- Modify: `typescript/apps/docs-server/src/server.ts`
- Modify: `typescript/apps/docs-server/src/index.ts`
- Test: `typescript/apps/docs-server/src/server.test.ts`

**Interfaces:**
- Consumes: `DocCommentStore.threadsForDoc` (Task 2); `PearClient.spaceUri`, `commentSpaceUri`, `check`, `listReadableSpaces` (Task 3 + existing); `NetworkHabitatDocsListComments.OutputSchema`, `NetworkHabitatDocsListDocs.OutputSchema` (Task 1).
- Produces: `createApp(config, pear, docs, meta, orgs, comments, verifier?)` — new `comments` param + optional injectable `verifier`; `listDocs` output now includes `commentSpace` per doc.

- [ ] **Step 1: Write the failing route test**

Create `typescript/apps/docs-server/src/server.test.ts`:

```ts
import { DatabaseSync } from "node:sqlite";
import { describe, expect, it } from "vitest";
import { DocCommentStore } from "./docCommentStore";
import { createApp } from "./server";
import type { DerivedConfig } from "./config";
import type { PearClient } from "./pearClient";
import type { DocCrdtStore } from "./docCrdtStore";
import type { DocMetadataStore } from "./docMetadataStore";
import type { OrgDirectory } from "./orgDirectory";
import type { ServiceAuthVerifier } from "./serviceAuth";

const CALLER = "did:web:alice";
const ORG = "did:web:org.example";
const DOC_ID = "doc1";
const DOC_SPACE = `ats://${ORG}/network.habitat.docs/${DOC_ID}`;
const COMMENT_SPACE = `ats://${ORG}/network.habitat.docs.comments/${DOC_ID}`;

const config = {
  domain: "docs-server.test",
  port: 0,
  db: ":memory:",
  did: "did:web:docs-server.test",
  serviceId: "docs",
  sapUrl: "http://sap.test",
} satisfies DerivedConfig;

const fakeVerifier = {
  verify: async () => CALLER,
} as unknown as ServiceAuthVerifier;

function harness(allowed: boolean) {
  const comments = new DocCommentStore(new DatabaseSync(":memory:"));
  const pear = {
    spaceUri: (skey: string, org: string) =>
      `ats://${org}/network.habitat.docs/${skey}`,
    commentSpaceUri: (skey: string, org: string) =>
      `ats://${org}/network.habitat.docs.comments/${skey}`,
    check: async () => allowed,
  } as unknown as PearClient;
  const orgs = { orgForUser: () => ORG } as unknown as OrgDirectory;
  const app = createApp(
    config,
    pear,
    {} as DocCrdtStore,
    {} as DocMetadataStore,
    orgs,
    comments,
    fakeVerifier,
  );
  return { app, comments };
}

function req(app: ReturnType<typeof harness>["app"]) {
  return app.request(
    `/xrpc/network.habitat.docs.listComments?docId=${DOC_ID}`,
    { headers: { Authorization: "Bearer test" } },
  );
}

describe("listComments route", () => {
  it("returns grouped threads for a doc the caller can read", async () => {
    const { app, comments } = harness(true);
    comments.upsertComment({
      docSpace: DOC_SPACE,
      uri: `${COMMENT_SPACE}/${CALLER}/network.habitat.docs.comment/c1`,
      author: CALLER,
      body: "hello",
      createdAt: "2026-07-07T00:00:00.000Z",
      range: { start: "s", end: "e" },
    });

    const res = await req(app);
    expect(res.status).toBe(200);
    const body = (await res.json()) as { comments: { body: string }[] };
    expect(body.comments).toHaveLength(1);
    expect(body.comments[0].body).toBe("hello");
  });

  it("rejects a caller who cannot read the comment space", async () => {
    const { app } = harness(false);
    const res = await req(app);
    expect(res.status).toBe(403);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm --filter docs-server test server`
Expected: FAIL — `createApp` signature and the route don't exist yet.

- [ ] **Step 3: Update `createApp` signature + imports**

In `typescript/apps/docs-server/src/server.ts`, add to the `from "api"` type import block:

```ts
  NetworkHabitatDocsListComments,
```

Add the store type import:

```ts
import type { DocCommentStore } from "./docCommentStore";
```

Change the signature and remove the inner `const verifier = ...`:

```ts
export function createApp(
  config: DerivedConfig,
  pear: PearClient,
  docs: DocCrdtStore,
  meta: DocMetadataStore,
  orgs: OrgDirectory,
  comments: DocCommentStore,
  verifier: ServiceAuthVerifier = new ServiceAuthVerifier(config),
): Hono {
  const app = new Hono();
```

- [ ] **Step 4: Add `commentSpace` to the listDocs output**

Replace the body of the existing `listDocs` route's response construction so each doc carries its derivable comment-space URI:

```ts
  app.get("/xrpc/network.habitat.docs.listDocs", async (c) => {
    const caller = await authorize(
      c.req.header("Authorization"),
      "network.habitat.docs.listDocs",
      verifier,
    );
    const spaces = await pear.listReadableSpaces(orgFor(caller), caller);
    const output: NetworkHabitatDocsListDocs.OutputSchema = {
      docs: meta.docsBySpaceUris(spaces).map((d) => ({
        ...d,
        // Owner DID is the 3rd URI segment of ats://<org>/<type>/<skey>.
        commentSpace: pear.commentSpaceUri(d.docId, d.uri.split("/")[2]),
      })),
    };
    return c.json(output);
  });
```

- [ ] **Step 5: Add the listComments route**

Add next to `listDocs`:

```ts
  // listComments returns a doc's comment threads. Authorization is against the
  // *comment* space (not the doc): comment-only members can read comments
  // without being doc members. The org proxy holds reader on the space, so the
  // check runs as the org on the caller's behalf.
  app.get("/xrpc/network.habitat.docs.listComments", async (c) => {
    const caller = await authorize(
      c.req.header("Authorization"),
      "network.habitat.docs.listComments",
      verifier,
    );
    const org = orgFor(caller);
    const docId = c.req.query("docId");
    if (!docId) {
      return c.json({ error: "InvalidRequest", message: "missing docId" }, 400);
    }
    const commentSpace = pear.commentSpaceUri(docId, org);
    const allowed = await pear.check(org, caller, "reader", commentSpace);
    if (!allowed) {
      throw new ForbiddenError("caller cannot read this doc's comments");
    }
    const output: NetworkHabitatDocsListComments.OutputSchema = {
      comments: comments.threadsForDoc(pear.spaceUri(docId, org)),
    };
    return c.json(output);
  });
```

- [ ] **Step 6: Run the route test to verify it passes**

Run: `pnpm --filter docs-server test server`
Expected: PASS — both cases green.

- [ ] **Step 7: Wire the store into index.ts**

In `typescript/apps/docs-server/src/index.ts`, add the import:

```ts
import { DocCommentStore } from "./docCommentStore";
```

Construct it after `const orgs = ...`:

```ts
  const comments = new DocCommentStore(db);
```

Pass it to the crawler and app:

```ts
  const crawler = new Crawler(config, meta, docs, orgs, comments);
```

```ts
  const app = createApp(config, pear, docs, meta, orgs, comments);
```

- [ ] **Step 8: Full typecheck + all docs-server tests**

Run: `pnpm --filter docs-server build && pnpm --filter docs-server test`
Expected: build passes; store, crawler, docCrdtStore, and server tests all green.

- [ ] **Step 9: Commit**

```bash
git add typescript/apps/docs-server/src/server.ts typescript/apps/docs-server/src/index.ts typescript/apps/docs-server/src/server.test.ts
git commit -m "feat(docs-server): listComments endpoint + docView.commentSpace"
```

---

## Task 6: Shared client endpoint + frontend anchor helpers

**Files:**
- Modify: `typescript/internal/src/habitatClient.ts`
- Create: `typescript/apps/docsv2/src/lib/anchor.ts`
- Test: `typescript/apps/docsv2/src/lib/anchor.test.ts`

**Interfaces:**
- Produces:
  - `query("network.habitat.docs.listComments", { docId }, opts)` is type-checked.
  - `interface CommentRange { start: string; end: string }`
  - `relPosToString(rel: Y.RelativePosition): string`, `stringToRelPos(s: string): Y.RelativePosition`
  - `rangeFromSelection(editor: Editor): CommentRange | null`
  - `resolveRange(editor: Editor, range: CommentRange): { from: number; to: number } | null`

- [ ] **Step 1: Register the query endpoint**

In `typescript/internal/src/habitatClient.ts`, add `NetworkHabitatDocsListComments` to the `from "api"` import block (next to `NetworkHabitatDocsListDocs`), then add to `QueryEndpoints` beside the existing `listDocs` entry:

```ts
  // Implemented by the docs server; reached via pear service proxying when
  // called with an Atproto-Proxy header.
  "network.habitat.docs.listComments": Query<
    NetworkHabitatDocsListComments.QueryParams,
    NetworkHabitatDocsListComments.OutputSchema
  >;
```

- [ ] **Step 2: Typecheck internal**

Run: `pnpm --filter internal build`
Expected: builds clean.

- [ ] **Step 3: Write the failing anchor round-trip test**

Create `typescript/apps/docsv2/src/lib/anchor.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import * as Y from "yjs";
import { relPosToString, stringToRelPos } from "./anchor";

describe("relative position serialization", () => {
  it("survives a JSON string round-trip and resolves to the same index", () => {
    const doc = new Y.Doc();
    const text = doc.getText("t");
    text.insert(0, "hello world");

    const rel = Y.createRelativePositionFromTypeIndex(text, 6);
    const restored = stringToRelPos(relPosToString(rel));
    const abs = Y.createAbsolutePositionFromRelativePosition(restored, doc);

    expect(abs?.index).toBe(6);
  });

  it("keeps pointing at the same content after an earlier insert", () => {
    const doc = new Y.Doc();
    const text = doc.getText("t");
    text.insert(0, "world");

    const rel = Y.createRelativePositionFromTypeIndex(text, 5);
    const s = relPosToString(rel);

    text.insert(0, "hello "); // shifts "world" right by 6
    const abs = Y.createAbsolutePositionFromRelativePosition(stringToRelPos(s), doc);

    expect(abs?.index).toBe(11);
  });
});
```

- [ ] **Step 4: Run the test to verify it fails**

Run: `pnpm --filter docsv2 test anchor`
Expected: FAIL — cannot resolve `./anchor`.

- [ ] **Step 5: Implement the anchor helpers**

Create `typescript/apps/docsv2/src/lib/anchor.ts`:

```ts
import * as Y from "yjs";
import {
  ySyncPluginKey,
  absolutePositionToRelativePosition,
  relativePositionToAbsolutePosition,
} from "@tiptap/y-tiptap";
import type { Editor } from "@tiptap/react";

// CommentRange is a serialized pair of Yjs relative positions bounding a
// commented span. Each endpoint is a JSON-encoded Y.RelativePosition, stable
// across concurrent edits so the highlight stays on the same text.
export interface CommentRange {
  start: string;
  end: string;
}

export function relPosToString(rel: Y.RelativePosition): string {
  return JSON.stringify(Y.relativePositionToJSON(rel));
}

export function stringToRelPos(s: string): Y.RelativePosition {
  return Y.createRelativePositionFromJSON(JSON.parse(s));
}

// ySync reads the y-tiptap sync plugin state, which exposes the shared
// Y.XmlFragment (`type`), its `doc`, and the ProseMirror<->Yjs `binding` (whose
// `mapping` converts positions). Returns null before the editor is bound.
function ySync(editor: Editor): {
  type: Y.XmlFragment;
  doc: Y.Doc;
  binding: { mapping: unknown };
} | null {
  const state = ySyncPluginKey.getState(editor.state);
  if (!state || !state.binding) {
    return null;
  }
  return state as {
    type: Y.XmlFragment;
    doc: Y.Doc;
    binding: { mapping: unknown };
  };
}

// rangeFromSelection converts the editor's current (non-empty) selection into a
// serialized comment range. Returns null for a collapsed selection or an
// unbound editor.
export function rangeFromSelection(editor: Editor): CommentRange | null {
  const { from, to } = editor.state.selection;
  if (from === to) {
    return null;
  }
  const sync = ySync(editor);
  if (!sync) {
    return null;
  }
  const start = absolutePositionToRelativePosition(from, sync.type, sync.binding.mapping);
  const end = absolutePositionToRelativePosition(to, sync.type, sync.binding.mapping);
  return { start: relPosToString(start), end: relPosToString(end) };
}

// resolveRange converts a stored comment range back into absolute ProseMirror
// positions against the editor's current document. Returns null if either
// endpoint can no longer be resolved (e.g. the anchored text was deleted).
export function resolveRange(
  editor: Editor,
  range: CommentRange,
): { from: number; to: number } | null {
  const sync = ySync(editor);
  if (!sync) {
    return null;
  }
  const from = relativePositionToAbsolutePosition(
    sync.doc,
    sync.type,
    stringToRelPos(range.start),
    sync.binding.mapping,
  );
  const to = relativePositionToAbsolutePosition(
    sync.doc,
    sync.type,
    stringToRelPos(range.end),
    sync.binding.mapping,
  );
  if (from == null || to == null) {
    return null;
  }
  return { from, to };
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `pnpm --filter docsv2 test anchor`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add typescript/internal/src/habitatClient.ts typescript/apps/docsv2/src/lib/anchor.ts typescript/apps/docsv2/src/lib/anchor.test.ts
git commit -m "feat(docsv2): register listComments client + Yjs anchor helpers"
```

---

## Task 7: Comment queries + mutations (frontend)

**Files:**
- Modify: `typescript/apps/docsv2/src/queries/docs.tsx`
- Create: `typescript/apps/docsv2/src/queries/comments.tsx`

**Interfaces:**
- Consumes: `docsProxyHeaders` (from `@/queries/docs`), `query`/`procedure`/`AuthManager` from `internal`, `CommentRange` (Task 6), `NetworkHabitatDocsListComments` (Task 1).
- Produces:
  - `DocSummary` gains `commentSpace?: string`.
  - `type CommentThread = NetworkHabitatDocsListComments.CommentView`
  - `listCommentsQueryOptions(docId, authManager)`
  - `createComment(authManager, commentSpace, { body, range, docSpace }): Promise<{ uri: string }>`
  - `createReply(authManager, commentSpace, { body, parent, docSpace }): Promise<{ uri: string }>`

- [ ] **Step 1: Surface `commentSpace` on DocSummary**

In `typescript/apps/docsv2/src/queries/docs.tsx`, add the field to the `DocSummary` interface:

```ts
export interface DocSummary {
  docId: string;
  uri: string;
  title: string;
  commentSpace?: string;
}
```

(The `docsListQueryOptions` `queryFn` already returns the server `docs` array verbatim, so `commentSpace` flows through with no other change.)

- [ ] **Step 2: Implement the comments queries + mutations**

Create `typescript/apps/docsv2/src/queries/comments.tsx`:

```tsx
import { queryOptions } from "@tanstack/react-query";
import { AuthManager, procedure, query } from "internal";
import type { NetworkHabitatDocsListComments } from "api";
import { docsProxyHeaders } from "@/queries/docs";
import type { CommentRange } from "@/lib/anchor";

const COMMENT_COLLECTION = "network.habitat.docs.comment";

export type CommentThread = NetworkHabitatDocsListComments.CommentView;

// listCommentsQueryOptions fetches a doc's comment threads from the docs server
// (proxied through pear, same as listDocs).
export const listCommentsQueryOptions = (
  docId: string,
  authManager: AuthManager,
) =>
  queryOptions({
    queryKey: ["comments", docId],
    queryFn: async (): Promise<CommentThread[]> => {
      const { comments } = await query(
        "network.habitat.docs.listComments",
        { docId },
        { authManager, headers: docsProxyHeaders() },
      );
      return comments;
    },
  });

// createComment writes a top-level comment into the doc's comment space using
// the member's own session. The rkey is omitted so pear assigns a TID.
export async function createComment(
  authManager: AuthManager,
  commentSpace: string,
  args: { body: string; range: CommentRange; docSpace: string },
): Promise<{ uri: string }> {
  return procedure(
    "network.habitat.space.putRecord",
    {
      space: commentSpace,
      collection: COMMENT_COLLECTION,
      record: {
        body: args.body,
        createdAt: new Date().toISOString(),
        docSpace: args.docSpace,
        range: args.range,
      },
    },
    { authManager },
  );
}

// createReply writes a reply into the comment space, referencing the parent
// comment's URI and omitting the range (replies inherit the parent's anchor).
export async function createReply(
  authManager: AuthManager,
  commentSpace: string,
  args: { body: string; parent: string; docSpace: string },
): Promise<{ uri: string }> {
  return procedure(
    "network.habitat.space.putRecord",
    {
      space: commentSpace,
      collection: COMMENT_COLLECTION,
      record: {
        body: args.body,
        createdAt: new Date().toISOString(),
        docSpace: args.docSpace,
        parent: args.parent,
      },
    },
    { authManager },
  );
}
```

- [ ] **Step 3: Typecheck**

Run: `pnpm --filter docsv2 build`
Expected: builds clean.

- [ ] **Step 4: Commit**

```bash
git add typescript/apps/docsv2/src/queries/docs.tsx typescript/apps/docsv2/src/queries/comments.tsx
git commit -m "feat(docsv2): comment list query + create mutations"
```

---

## Task 8: Comments sidebar, composer, and highlights

**Files:**
- Create: `typescript/apps/docsv2/src/extensions/commentHighlight.ts`
- Create: `typescript/apps/docsv2/src/components/CommentsSidebar.tsx`
- Test: `typescript/apps/docsv2/src/components/CommentsSidebar.test.tsx`
- Modify: `typescript/apps/docsv2/src/routes/_requireAuth/$uri.tsx`
- Modify: `typescript/apps/docsv2/src/index.css`

**Interfaces:**
- Consumes: `CommentThread` + queries/mutations (Task 7), `resolveRange`/`rangeFromSelection`/`CommentRange` (Task 6), `DocSummary.commentSpace` (Task 7), the Tiptap `Editor` + `ydoc`.
- Produces: `CommentHighlight` extension + `setCommentHighlights(editor, ranges)`, `<CommentsSidebar>`.

- [ ] **Step 1: Implement the highlight extension**

Create `typescript/apps/docsv2/src/extensions/commentHighlight.ts`:

```ts
import { Extension } from "@tiptap/react";
import type { Editor } from "@tiptap/react";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import { resolveRange, type CommentRange } from "@/lib/anchor";

export interface HighlightRange {
  id: string;
  range: CommentRange;
}

const highlightKey = new PluginKey<HighlightRange[]>("comment-highlights");

// CommentHighlight paints an inline decoration over each comment's anchored
// range. Ranges are stored in plugin state and re-resolved from their Yjs
// relative positions on every editor state, so highlights track edits.
export const CommentHighlight = Extension.create({
  name: "commentHighlight",
  addProseMirrorPlugins() {
    const editor = this.editor;
    return [
      new Plugin<HighlightRange[]>({
        key: highlightKey,
        state: {
          init: () => [],
          apply(tr, value) {
            const next = tr.getMeta(highlightKey) as
              | HighlightRange[]
              | undefined;
            return next ?? value;
          },
        },
        props: {
          decorations(state) {
            const ranges = highlightKey.getState(state) ?? [];
            const decos: Decoration[] = [];
            for (const { id, range } of ranges) {
              const resolved = resolveRange(editor, range);
              if (!resolved || resolved.from >= resolved.to) {
                continue;
              }
              decos.push(
                Decoration.inline(resolved.from, resolved.to, {
                  class: "comment-highlight",
                  "data-comment-id": id,
                }),
              );
            }
            return DecorationSet.create(state.doc, decos);
          },
        },
      }),
    ];
  },
});

// setCommentHighlights replaces the set of highlighted ranges.
export function setCommentHighlights(
  editor: Editor,
  ranges: HighlightRange[],
): void {
  editor.view.dispatch(editor.state.tr.setMeta(highlightKey, ranges));
}
```

- [ ] **Step 2: Add the highlight style**

Append to `typescript/apps/docsv2/src/index.css`:

```css
.comment-highlight {
  background-color: #fff3bf;
  border-bottom: 2px solid #f2c94c;
  cursor: pointer;
}
```

- [ ] **Step 3: Write the failing sidebar render test**

Create `typescript/apps/docsv2/src/components/CommentsSidebar.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { CommentsSidebar } from "./CommentsSidebar";
import type { CommentThread } from "@/queries/comments";

const threads: CommentThread[] = [
  {
    uri: "ats://org/network.habitat.docs.comments/doc1/did:web:alice/network.habitat.docs.comment/c1",
    author: "did:web:alice",
    body: "top level",
    createdAt: "2026-07-07T00:00:00.000Z",
    range: { start: "s", end: "e" },
    replies: [
      {
        uri: "ats://org/network.habitat.docs.comments/doc1/did:web:bob/network.habitat.docs.comment/r1",
        author: "did:web:bob",
        body: "a reply",
        createdAt: "2026-07-07T00:00:01.000Z",
      },
    ],
  },
];

describe("CommentsSidebar", () => {
  it("renders comments and replies", () => {
    render(
      <CommentsSidebar
        threads={threads}
        canComment
        onSelect={() => {}}
        onReply={() => {}}
        isReplying={null}
      />,
    );
    expect(screen.getByText("top level")).toBeTruthy();
    expect(screen.getByText("a reply")).toBeTruthy();
  });

  it("calls onSelect with the thread when a comment is clicked", () => {
    const onSelect = vi.fn();
    render(
      <CommentsSidebar
        threads={threads}
        canComment
        onSelect={onSelect}
        onReply={() => {}}
        isReplying={null}
      />,
    );
    fireEvent.click(screen.getByText("top level"));
    expect(onSelect).toHaveBeenCalledWith(threads[0]);
  });
});
```

- [ ] **Step 4: Run the test to verify it fails**

Run: `pnpm --filter docsv2 test CommentsSidebar`
Expected: FAIL — cannot resolve `./CommentsSidebar`.

- [ ] **Step 5: Implement the sidebar**

Create `typescript/apps/docsv2/src/components/CommentsSidebar.tsx`:

```tsx
import { useState } from "react";
import { Button } from "internal/components/ui";
import type { CommentThread } from "@/queries/comments";

interface CommentsSidebarProps {
  threads: CommentThread[];
  // canComment gates the reply boxes (member has write access to comments).
  canComment: boolean;
  // onSelect scrolls the editor to the thread's anchored range.
  onSelect: (thread: CommentThread) => void;
  // onReply submits a reply body for the given top-level comment URI.
  onReply: (parentUri: string, body: string) => void;
  // isReplying is the parent URI whose reply is submitting, or null.
  isReplying: string | null;
}

function shortDid(did: string): string {
  return did.replace(/^did:web:/, "");
}

export function CommentsSidebar({
  threads,
  canComment,
  onSelect,
  onReply,
  isReplying,
}: CommentsSidebarProps) {
  return (
    <aside className="w-80 shrink-0 border-l overflow-y-auto p-3 flex flex-col gap-3">
      <h2 className="text-sm font-medium text-muted-foreground">Comments</h2>
      {threads.length === 0 && (
        <p className="text-sm text-muted-foreground">No comments yet.</p>
      )}
      {threads.map((thread) => (
        <div key={thread.uri} className="rounded border p-2 flex flex-col gap-2">
          <button
            type="button"
            className="text-left"
            onClick={() => onSelect(thread)}
          >
            <div className="text-xs text-muted-foreground">
              {shortDid(thread.author)}
            </div>
            <div className="text-sm">{thread.body}</div>
          </button>
          {thread.replies.map((reply) => (
            <div key={reply.uri} className="ml-3 border-l pl-2">
              <div className="text-xs text-muted-foreground">
                {shortDid(reply.author)}
              </div>
              <div className="text-sm">{reply.body}</div>
            </div>
          ))}
          {canComment && (
            <ReplyBox
              disabled={isReplying === thread.uri}
              onSubmit={(body) => onReply(thread.uri, body)}
            />
          )}
        </div>
      ))}
    </aside>
  );
}

function ReplyBox({
  onSubmit,
  disabled,
}: {
  onSubmit: (body: string) => void;
  disabled: boolean;
}) {
  const [value, setValue] = useState("");
  return (
    <form
      className="flex gap-1"
      onSubmit={(e) => {
        e.preventDefault();
        const body = value.trim();
        if (!body) return;
        onSubmit(body);
        setValue("");
      }}
    >
      <input
        className="flex-1 rounded border px-2 py-1 text-sm outline-none"
        placeholder="Reply…"
        value={value}
        onChange={(e) => setValue(e.target.value)}
      />
      <Button type="submit" size="sm" disabled={disabled}>
        Reply
      </Button>
    </form>
  );
}
```

- [ ] **Step 6: Run the sidebar test to verify it passes**

Run: `pnpm --filter docsv2 test CommentsSidebar`
Expected: PASS.

- [ ] **Step 7: Wire into the doc route**

In `typescript/apps/docsv2/src/routes/_requireAuth/$uri.tsx`:

7a. Extend/add imports (merge into existing `react` and `@tanstack/react-query` imports, don't duplicate):

```tsx
import { useEffect } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listCommentsQueryOptions,
  createComment,
  createReply,
  type CommentThread,
} from "@/queries/comments";
import {
  CommentHighlight,
  setCommentHighlights,
} from "@/extensions/commentHighlight";
import { rangeFromSelection, resolveRange } from "@/lib/anchor";
import { CommentsSidebar } from "@/components/CommentsSidebar";
```

7b. **Selection state (before `useEditor`).** Next to `const [dirty, setDirty] = useState(false);` add:

```tsx
    const [hasSelection, setHasSelection] = useState(false);
    const [replyingTo, setReplyingTo] = useState<string | null>(null);
```

7c. **Resolve the comment space.** Replace the existing `spaceUri` derivation so both the doc space and comment space are available:

```tsx
    const { data: docs } = useQuery(docsListQueryOptions(authManager));
    const doc = docs?.find((d) => d.docId === docId);
    const spaceUri = doc?.uri;
    const commentSpace = doc?.commentSpace;
```

7d. Register the highlight extension in `useEditor`'s `extensions` array, after `Collaboration.configure(...)`:

```tsx
          Collaboration.configure({ document: ydoc }),
          CommentHighlight,
```

7e. Add an `onSelectionUpdate` handler next to `onUpdate` in the `useEditor` options (closes over `setHasSelection`):

```tsx
        onSelectionUpdate({ editor }) {
          const { from, to } = editor.state.selection;
          setHasSelection(from !== to);
        },
```

7f. **Comments query, mutations, highlight effect (all AFTER `const editor = useEditor(...)`)** — everything here reads `editor`, so it must follow the `useEditor` call:

```tsx
    const queryClient = useQueryClient();
    const { data: threads } = useQuery(
      listCommentsQueryOptions(docId, authManager),
    );

    const addComment = useMutation({
      mutationFn: async () => {
        if (!editor || !commentSpace || !spaceUri) return;
        const range = rangeFromSelection(editor);
        if (!range) return;
        const body = window.prompt("Comment");
        if (!body) return;
        await createComment(authManager, commentSpace, {
          body,
          range,
          docSpace: spaceUri,
        });
      },
      onSuccess: () =>
        queryClient.invalidateQueries(
          listCommentsQueryOptions(docId, authManager),
        ),
    });

    const replyMutation = useMutation({
      mutationFn: async ({
        parent,
        body,
      }: {
        parent: string;
        body: string;
      }) => {
        if (!commentSpace || !spaceUri) return;
        setReplyingTo(parent);
        await createReply(authManager, commentSpace, {
          body,
          parent,
          docSpace: spaceUri,
        });
      },
      onSettled: () => setReplyingTo(null),
      onSuccess: () =>
        queryClient.invalidateQueries(
          listCommentsQueryOptions(docId, authManager),
        ),
    });

    useEffect(() => {
      if (!editor || !threads) return;
      setCommentHighlights(
        editor,
        threads
          .filter((t) => t.range)
          .map((t) => ({ id: t.uri, range: t.range! })),
      );
    }, [editor, threads]);

    const onSelectComment = (thread: CommentThread) => {
      if (!editor || !thread.range) return;
      const resolved = resolveRange(editor, thread.range);
      if (!resolved) return;
      editor.chain().focus().setTextSelection(resolved).scrollIntoView().run();
    };
```

7g. Replace the component's returned JSX (editor + sidebar side by side; add a Comment button):

```tsx
    return (
      <div className="flex flex-col-reverse h-full">
        <div className="flex-1 flex min-h-0">
          <div className="flex-1 flex flex-col items-center overflow-y-auto [&_.ProseMirror]:focus-visible:outline-2 [&_.ProseMirror]:focus-visible:outline-offset-[-1px] [&_.ProseMirror]:focus-visible:outline-ring/40">
            <EditorContent className="w-full flex-1" editor={editor} />
          </div>
          <CommentsSidebar
            threads={threads ?? []}
            canComment={!!commentSpace}
            onSelect={onSelectComment}
            onReply={(parent, body) => replyMutation.mutate({ parent, body })}
            isReplying={replyingTo}
          />
        </div>
        <PageHeader>
          <div className="flex items-center gap-2">
            {spaceUri && (
              <ShareDialogV2 spaceUri={spaceUri} authManager={authManager} />
            )}
            <Button
              size="sm"
              variant="outline"
              disabled={!commentSpace || !hasSelection || addComment.isPending}
              onClick={() => addComment.mutate()}
            >
              Comment
            </Button>
            <Popover>
              <PopoverTrigger
                render={
                  <Button size="icon" variant="outline">
                    {dirty ? <Spinner /> : <CheckIcon />}
                  </Button>
                }
              />
              <PopoverContent>
                <PopoverTitle>Sync status</PopoverTitle>
                <span>{dirty ? "🔄 Syncing" : "✅ Synced"}</span>
              </PopoverContent>
            </Popover>
          </div>
          <HelpDialog />
        </PageHeader>
      </div>
    );
```

(Remove the old single-column editor container and the old `const { data: docs } ... spaceUri` lines replaced in 7c.)

- [ ] **Step 8: Typecheck + all docsv2 tests**

Run: `pnpm --filter docsv2 build && pnpm --filter docsv2 test`
Expected: build passes; anchor + sidebar tests green.

- [ ] **Step 9: Lint + format**

Run: `moon docsv2:format && moon docsv2:lint-check`
Expected: pass.

- [ ] **Step 10: Commit**

```bash
git add typescript/apps/docsv2/src/extensions/commentHighlight.ts typescript/apps/docsv2/src/components/CommentsSidebar.tsx typescript/apps/docsv2/src/components/CommentsSidebar.test.tsx typescript/apps/docsv2/src/routes/_requireAuth/\$uri.tsx typescript/apps/docsv2/src/index.css
git commit -m "feat(docsv2): comments sidebar, composer, and highlights"
```

---

## Task 9: End-to-end manual verification

**Files:** none (verification only). Confirms the full loop, the derived permissions, and sap→crawler indexing, which can't be exercised in unit tests.

- [ ] **Step 1: Start the stack**

Run: `moon docsv2:dev`
Expected: pear, sap, caddy, docs-server, and the docsv2 frontend come up. Wait for `[docs-server] listening` and the frontend URL.

- [ ] **Step 2: Create a doc and author a comment**

Open/create a doc, select a span, click **Comment**, enter a body, submit.
Expected: within a couple seconds (crawler index + refetch) the span gets the yellow `comment-highlight` underline and the comment shows in the sidebar. This confirms the creator (doc owner → comment writer via the userset) can write to the comment space.

- [ ] **Step 3: Anchoring survives edits**

Type text *before* the highlighted span.
Expected: the highlight moves with its original text.

- [ ] **Step 4: Reply**

Type into a comment's reply box and submit.
Expected: the reply nests under its parent.

- [ ] **Step 5: Click-to-scroll**

Scroll away, click a comment in the sidebar.
Expected: the editor selects + scrolls to the anchored range.

- [ ] **Step 6: Derived read access (second member)**

Share the doc with a second member as a **reader** via the share dialog. As that member, open the doc.
Expected: they can see existing comments (doc reader → comment reader via the userset), and the **Comment** button / reply boxes behave per their access. A doc **writer** should be able to add comments; a pure **reader** can view but their `putRecord` will be denied by pear (accepted limitation).

- [ ] **Step 7: No regressions**

Run: `moon :lint-check` and `pnpm --filter docs-server test && pnpm --filter docsv2 test`
Expected: all green.

---

## Notes, assumptions, and out-of-scope

- **Comment-only access (write on comment space, no doc access)** is enabled by the architecture — write a direct `writer` tuple on the comment space (via `pear.writeUsersetTuple`/`grantRole` proxied as the org). The **UI** to invite a comment-only collaborator is *not* built here; add a "commenter" option to the share flow in a follow-up.
- **Read-only doc members cannot comment**: they get comment-space *read* (via the reader userset) but not write, so `putRecord` is denied. Giving them comment-write means an explicit writer tuple on the comment space.
- **Anti-spoofing:** the crawler derives a comment's doc space from the comment record's *own* space URI (whose skey it shares), not from the record's `docSpace` field, so a member can't attribute a comment to a doc whose comment space they can't write.
- **Comment deletion / editing** isn't handled (crawler processes upserts only; no delete UI). Re-`putRecord` at the same rkey would overwrite by URI, but no edit affordance is built.
- **Indexing latency:** new comments appear after sap delivers the record to the crawler; optimistic cache insertion was intentionally left out to keep the store the single source of truth.
- **Orphan replies** (parent not yet indexed) surface as top-level comments so nothing is lost; they re-nest on the next `threadsForDoc` rebuild once the parent is indexed.
```