import { createServerFn } from "@tanstack/react-start";
import { Effect, Schema } from "effect";
import {
  commentsForDoc,
  deleteDocAccess,
  deleteDocOrgAccess,
  docByUri,
  docsFor,
  repliesForDoc,
  upsertDoc,
  upsertDocAccess,
  upsertDocOrgAccess,
  type DocSummary,
} from "@/db";
import {
  commentsSpaceUri,
  ensureCommentsSpace,
  removeComment,
  removeReply,
  toCommentReplyView,
  toCommentView,
  writeComment,
  writeReply,
  type CommentReplyView,
  type CommentView,
  type StrongRef,
} from "@/server/docs/comments.server";
import {
  createDocSpace,
  deleteUserGrant,
  docRole,
  listUserGrants,
  orgMembersSpaceUri,
  type DocRole,
} from "@/server/docs/docs.server";
import { BadRequest, Forbidden } from "@/server/effect/errors";
import { currentMember, requireOrg } from "@/server/effect/member";
import { runEffect } from "@/server/effect/run";
import { AppEnv, attempt, Database, Habitat } from "@/server/effect/services";
import { watchSpaces } from "@/server/sync";
import { requireConnected } from "@/server/groups";

// Docs: Chalk's collaborative documents. Each doc is its own space owned by
// the group, edited live through its DocRoom (a Durable Object holding the
// doc's Yjs state), and mirrored into D1 by the syncer so listing docs is
// one query. Sharing is network.habitat.relationship grants on the doc and
// its companion comments space.

export type { CommentReplyView, CommentView, DocRole, DocSummary, StrongRef };

const OrgInput = Schema.Struct({ org: Schema.String });
const DocInput = Schema.Struct({ org: Schema.String, docId: Schema.String });

// The tiers a doc grantee can hold, and the relation each is stored as:
//
//   editor    -> manager on the doc space
//   commenter -> writer  on the comments space
//   viewer    -> reader  on the doc space
//
// Editors get "manager" so they can share the doc themselves (pear's
// setUserRelation requires it). A commenter is granted on the comments
// space alone, and reads the doc because the comments space's writers are
// readers of the doc space.
const ROLE_TO_GRANT: Record<
  DocRole,
  { relation: "manager" | "writer" | "reader"; onComments: boolean }
> = {
  editor: { relation: "manager", onComments: false },
  commenter: { relation: "writer", onComments: true },
  viewer: { relation: "reader", onComments: false },
};

// grantSpaces returns the two spaces a doc's grants can live on: its own
// and its comments space.
function grantSpaces(docId: string): { space: string; onComments: boolean }[] {
  const commentsSpace = commentsSpaceUri(docId);
  return [
    { space: docId, onComments: false },
    ...(commentsSpace ? [{ space: commentsSpace, onComments: true }] : []),
  ];
}

// relationToRole maps a stored relation back to the tier it represents; the
// space it was found on is part of the answer.
function relationToRole(
  relation: string,
  onComments: boolean,
): DocRole | undefined {
  if (onComments) return relation === "writer" ? "commenter" : undefined;
  if (relation === "manager" || relation === "writer") return "editor";
  if (relation === "reader") return "viewer";
  return undefined;
}

function canComment(role: DocRole | null): boolean {
  return role === "editor" || role === "commenter";
}

// roleOn resolves the member's tier on a doc.
const roleOn = Effect.fn("docs.roleOn")(function* (did: string, docId: string) {
  const habitat = yield* Habitat;
  return yield* attempt(did, "docs.docRole", () =>
    docRole(habitat.client(did), did, docId),
  );
});

// requireDocInGroup checks the doc belongs to the group the page is in, so
// a group's pages can't be pointed at another group's doc.
const requireDocInGroup = Effect.fn("docs.requireDocInGroup")(function* (
  org: string,
  docId: string,
) {
  const db = yield* Database;
  const doc = yield* Effect.promise(() => docByUri(db, docId));
  if (doc && doc.ownerDid !== org) {
    return yield* new BadRequest({
      message: "That doc belongs to another group",
    });
  }
  return doc;
});

export const listDocs = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(OrgInput))
  .handler(({ data }) =>
    runEffect(
      "docs.list",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const db = yield* Database;
        return yield* Effect.promise(() => docsFor(db, did, org));
      }),
    ),
  );

// createDoc creates a doc in the group: its space (owned by the group), a
// manager grant for its creator, its comments space, and the local rows
// that make it show up in the creator's list before the syncer catches up.
export const createDoc = createServerFn({ method: "POST" })
  .validator(Schema.decodeUnknownSync(OrgInput))
  .handler(({ data }) =>
    runEffect(
      "docs.create",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        // Sharing an org doc goes through the group's own session.
        yield* requireConnected(org);
        const habitat = yield* Habitat;
        const env = yield* AppEnv;
        const db = yield* Database;
        const client = habitat.client(did);

        const { uri, ownerDid } = yield* attempt(did, "docs.createSpace", () =>
          createDocSpace(client, did, org),
        );
        yield* Effect.annotateCurrentSpan({ doc: uri });

        // An org doc space is created with no opensocial roles, so its
        // creator holds nothing on it yet. The group owns the space, so its
        // session can grant them manager.
        yield* habitat.call(
          org,
          "network.habitat.relationship.setUserRelation",
          "POST",
          {
            subject: did,
            relation: "manager",
            space: uri,
          },
        );

        yield* Effect.promise(() => watchSpaces(env, [uri]));
        // The comments space exists from the start, so the doc can be
        // shared with commenters right away.
        yield* attempt(did, "docs.ensureCommentsSpace", () =>
          ensureCommentsSpace(client, uri, { ownerDid, isOrg: true }),
        );
        yield* Effect.promise(async () => {
          await upsertDoc(db, {
            spaceUri: uri,
            docId: uri,
            ownerDid,
            title: "Untitled",
          });
          await upsertDocAccess(db, {
            uri,
            spaceUri: uri,
            subjectDid: did,
            relation: "manager",
          });
          // Record the room's identity now, so the owner-republish alarm
          // knows the owner before the syncer delivers it.
          await env.DOC.get(env.DOC.idFromName(uri)).seedIdentity({
            spaceUri: uri,
            ownerDid,
          });
        });
        yield* Effect.logInfo("created doc", { doc: uri });
        return { docId: uri, uri };
      }),
    ),
  );

export const getDocRole = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(DocInput))
  .handler(({ data }) =>
    runEffect(
      "docs.role",
      Effect.gen(function* () {
        const { did } = yield* requireOrg(data.org);
        return yield* roleOn(did, data.docId);
      }),
    ),
  );

// getDocInitialState returns the doc's current Yjs state so the editor
// renders with content before its WebSocket connects. DocRoom itself has no
// ACL, so this checks the member's tier first.
export const getDocInitialState = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(DocInput))
  .handler(({ data }) =>
    runEffect(
      "docs.initialState",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        yield* requireDocInGroup(org, data.docId);
        if (!(yield* roleOn(did, data.docId))) {
          return yield* new Forbidden({
            message: "You don't have access to this doc",
          });
        }
        const env = yield* AppEnv;
        return yield* Effect.promise(() =>
          env.DOC.get(env.DOC.idFromName(data.docId)).snapshot(),
        );
      }),
    ),
  );

export const getDocSummary = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(DocInput))
  .handler(({ data }) =>
    runEffect(
      "docs.summary",
      Effect.gen(function* () {
        yield* requireOrg(data.org);
        const doc = yield* requireDocInGroup(data.org, data.docId);
        return doc ?? null;
      }),
    ),
  );

// listDocAccess lists every user with a direct grant on the doc and the
// tier it gives them, read with the group's session (the group owns the
// doc, so it can always list them).
export const listDocAccess = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(DocInput))
  .handler(({ data }) =>
    runEffect(
      "docs.listAccess",
      Effect.gen(function* () {
        const { org } = yield* requireOrg(data.org);
        yield* requireDocInGroup(org, data.docId);
        const habitat = yield* Habitat;
        const perSpace = yield* Effect.forEach(
          grantSpaces(data.docId),
          ({ space, onComments }) =>
            attempt(org, "docs.listUserGrants", () =>
              listUserGrants(habitat.client(org), space),
            ).pipe(
              Effect.map((relations) =>
                relations.flatMap((r) => {
                  const role = relationToRole(r.relation, onComments);
                  return role ? [{ did: r.subject, role }] : [];
                }),
              ),
            ),
          { concurrency: "unbounded" },
        );
        return perSpace.flat();
      }),
    ),
  );

const ShareInput = Schema.Struct({
  org: Schema.String,
  docId: Schema.String,
  subjectDid: Schema.String,
  role: Schema.Literals(["editor", "commenter", "viewer"]),
});

// shareDoc grants a user a tier on the doc, replacing whatever tier they
// held before (which may live on the other of the doc's two spaces).
export const shareDoc = createServerFn({ method: "POST" })
  .validator(Schema.decodeUnknownSync(ShareInput))
  .handler(({ data }) =>
    runEffect(
      "docs.share",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        yield* requireDocInGroup(org, data.docId);
        const role = (yield* roleOn(did, data.docId)) ?? null;
        if (role !== "editor") {
          return yield* new Forbidden({
            message: "Only editors can share this doc",
          });
        }
        const habitat = yield* Habitat;
        const db = yield* Database;
        const { relation, onComments } = ROLE_TO_GRANT[data.role];
        const space = onComments ? commentsSpaceUri(data.docId) : data.docId;
        if (!space) return yield* new BadRequest({ message: "Invalid doc" });

        if (onComments) {
          yield* attempt(did, "docs.ensureCommentsSpace", () =>
            ensureCommentsSpace(habitat.client(did), data.docId, {
              ownerDid: org,
              isOrg: true,
            }),
          );
        }
        const { uri } = yield* habitat.call<{ uri: string }>(
          org,
          "network.habitat.relationship.setUserRelation",
          "POST",
          { subject: data.subjectDid, relation, space },
        );
        // Clear the grant on the other space, written after the new one so
        // the subject is never briefly without access.
        const stale = grantSpaces(data.docId).find((s) => s.space !== space);
        const staleUri = stale
          ? yield* attempt(org, "docs.deleteUserGrant", () =>
              deleteUserGrant(
                habitat.client(org),
                stale.space,
                data.subjectDid,
              ),
            )
          : undefined;
        // Update the mirror now rather than waiting on the syncer, so the
        // doc shows up in the subject's list right away.
        yield* Effect.promise(async () => {
          if (staleUri) await deleteDocAccess(db, staleUri);
          await upsertDocAccess(db, {
            uri,
            spaceUri: data.docId,
            subjectDid: data.subjectDid,
            relation,
          });
        });
        yield* Effect.logInfo("shared doc", {
          doc: data.docId,
          subject: data.subjectDid,
          role: data.role,
        });
      }),
    ),
  );

export const revokeDocAccess = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({
        org: Schema.String,
        docId: Schema.String,
        subjectDid: Schema.String,
      }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "docs.revoke",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        yield* requireDocInGroup(org, data.docId);
        if ((yield* roleOn(did, data.docId)) !== "editor") {
          return yield* new Forbidden({
            message: "Only editors can change who has access",
          });
        }
        const habitat = yield* Habitat;
        const db = yield* Database;
        // Sweep both spaces: leaving a stale grant on either would keep
        // access alive.
        for (const { space } of grantSpaces(data.docId)) {
          const uri = yield* attempt(org, "docs.deleteUserGrant", () =>
            deleteUserGrant(habitat.client(org), space, data.subjectDid),
          );
          if (uri) yield* Effect.promise(() => deleteDocAccess(db, uri));
        }
      }),
    ),
  );

// --- Sharing with the whole group ----------------------------------------

// The group-wide grant is "reader"/"writer", never "manager": letting every
// member reshare would be too broad.
const ORG_ROLE_TO_RELATION: Record<"editor" | "viewer", "writer" | "reader"> = {
  editor: "writer",
  viewer: "reader",
};

interface SpaceRelationView {
  uri: string;
  subject: string;
  relation: string;
}

// groupRelation finds the doc's spaceRelation naming the group's members
// space, the record behind "share with everyone in the group".
const groupRelation = Effect.fn("docs.groupRelation")(function* (
  org: string,
  docId: string,
) {
  const habitat = yield* Habitat;
  const { relations } = yield* habitat.call<{ relations: SpaceRelationView[] }>(
    org,
    "network.habitat.relationship.listRelations",
    "GET",
    { space: docId, subjectType: "space" },
  );
  const members = orgMembersSpaceUri(org);
  return relations.find((r) => r.subject === members);
});

export const getDocOrgAccess = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(DocInput))
  .handler(({ data }) =>
    runEffect(
      "docs.groupAccess",
      Effect.gen(function* () {
        const { org } = yield* requireOrg(data.org);
        const relation = yield* groupRelation(org, data.docId);
        if (!relation) return null;
        return relation.relation === "writer"
          ? ("editor" as const)
          : ("viewer" as const);
      }),
    ),
  );

export const shareDocWithOrg = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({
        org: Schema.String,
        docId: Schema.String,
        role: Schema.Literals(["editor", "viewer"]),
      }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "docs.shareWithGroup",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        yield* requireDocInGroup(org, data.docId);
        if ((yield* roleOn(did, data.docId)) !== "editor") {
          return yield* new Forbidden({
            message: "Only editors can share this doc",
          });
        }
        const habitat = yield* Habitat;
        const db = yield* Database;
        const { uri } = yield* habitat.call<{ uri: string }>(
          org,
          "network.habitat.relationship.setSpaceRelation",
          "POST",
          {
            subject: orgMembersSpaceUri(org),
            subjectRole: "reader",
            relation: ORG_ROLE_TO_RELATION[data.role],
            space: data.docId,
          },
        );
        yield* Effect.promise(() =>
          upsertDocOrgAccess(db, {
            uri,
            spaceUri: data.docId,
            orgDid: org,
            relation: ORG_ROLE_TO_RELATION[data.role],
          }),
        );
      }),
    ),
  );

export const revokeDocOrgAccess = createServerFn({ method: "POST" })
  .validator(Schema.decodeUnknownSync(DocInput))
  .handler(({ data }) =>
    runEffect(
      "docs.unshareWithGroup",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        if ((yield* roleOn(did, data.docId)) !== "editor") {
          return yield* new Forbidden({
            message: "Only editors can change who has access",
          });
        }
        const relation = yield* groupRelation(org, data.docId);
        if (!relation) return;
        const habitat = yield* Habitat;
        const db = yield* Database;
        yield* habitat.call(
          org,
          "network.habitat.relationship.deleteRelation",
          "POST",
          {
            uri: relation.uri,
          },
        );
        yield* Effect.promise(() => deleteDocOrgAccess(db, relation.uri));
      }),
    ),
  );

// --- Comments ------------------------------------------------------------

export interface CommentsPayload {
  comments: CommentView[];
}

// listComments returns a doc's threads from the D1 mirror, each root
// comment with its replies nested on it. A reply whose root is gone has no
// anchor to show against and is left out.
export const listComments = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(DocInput))
  .handler(({ data }) =>
    runEffect(
      "docs.listComments",
      Effect.gen(function* () {
        const { did } = yield* requireOrg(data.org);
        if (!(yield* roleOn(did, data.docId))) {
          return { comments: [] } satisfies CommentsPayload;
        }
        const db = yield* Database;
        const [comments, replies] = yield* Effect.promise(() =>
          Promise.all([
            commentsForDoc(db, data.docId),
            repliesForDoc(db, data.docId),
          ]),
        );
        const repliesByRoot = new Map<string, CommentReplyView[]>();
        for (const reply of replies) {
          const forRoot = repliesByRoot.get(reply.commentUri) ?? [];
          forRoot.push(toCommentReplyView(reply));
          repliesByRoot.set(reply.commentUri, forRoot);
        }
        return {
          comments: comments.map((c) =>
            toCommentView(c, { replies: repliesByRoot.get(c.uri) ?? [] }),
          ),
        } satisfies CommentsPayload;
      }),
    ),
  );

const requireCommenter = Effect.fn("docs.requireCommenter")(function* (
  did: string,
  docId: string,
) {
  if (!canComment(yield* roleOn(did, docId))) {
    return yield* new Forbidden({ message: "You can't comment on this doc" });
  }
});

// createComment starts a thread. anchorStart/anchorEnd are Yjs relative
// positions the client computed from its live Y.Doc.
export const createComment = createServerFn({ method: "POST" })
  .validator(
    (input: {
      org: string;
      docId: string;
      body: string;
      anchorStart: Uint8Array;
      anchorEnd: Uint8Array;
      quotedText?: string;
    }) => input,
  )
  .handler(({ data }) =>
    runEffect(
      "docs.createComment",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        yield* requireCommenter(did, data.docId);
        const habitat = yield* Habitat;
        const db = yield* Database;
        const doc = yield* Effect.promise(() => docByUri(db, data.docId));
        const ownerDid = doc?.ownerDid ?? org;
        return yield* attempt(did, "docs.writeComment", () =>
          writeComment(habitat.client(did), db, did, data.docId, {
            body: data.body,
            anchorStart: data.anchorStart,
            anchorEnd: data.anchorEnd,
            quotedText: data.quotedText,
            ownerDid,
            isOrg: ownerDid === org,
          }),
        );
      }),
    ),
  );

export const createReply = createServerFn({ method: "POST" })
  .validator(
    (input: { org: string; docId: string; comment: StrongRef; body: string }) =>
      input,
  )
  .handler(({ data }) =>
    runEffect(
      "docs.createReply",
      Effect.gen(function* () {
        const { did } = yield* requireOrg(data.org);
        yield* requireCommenter(did, data.docId);
        const habitat = yield* Habitat;
        const db = yield* Database;
        return yield* attempt(did, "docs.writeReply", () =>
          writeReply(habitat.client(did), db, did, data.docId, {
            comment: data.comment,
            body: data.body,
          }),
        );
      }),
    ),
  );

const RecordInput = Schema.Struct({
  org: Schema.String,
  docId: Schema.String,
  uri: Schema.String,
});

// Deleting a comment or reply: removeComment/removeReply enforce that only
// its author can.
export const deleteComment = createServerFn({ method: "POST" })
  .validator(Schema.decodeUnknownSync(RecordInput))
  .handler(({ data }) =>
    runEffect(
      "docs.deleteComment",
      Effect.gen(function* () {
        const did = yield* currentMember;
        const habitat = yield* Habitat;
        const db = yield* Database;
        yield* attempt(did, "docs.removeComment", () =>
          removeComment(habitat.client(did), db, did, data.uri),
        );
      }),
    ),
  );

export const deleteReply = createServerFn({ method: "POST" })
  .validator(Schema.decodeUnknownSync(RecordInput))
  .handler(({ data }) =>
    runEffect(
      "docs.deleteReply",
      Effect.gen(function* () {
        const did = yield* currentMember;
        const habitat = yield* Habitat;
        const db = yield* Database;
        yield* attempt(did, "docs.removeReply", () =>
          removeReply(habitat.client(did), db, did, data.uri),
        );
      }),
    ),
  );
