import { createServerFn } from "@tanstack/react-start";
import type { DidString } from "@atproto/syntax";
import { env } from "cloudflare:workers";
import {
  commentsForDoc,
  connectedOrgNames,
  deleteDocAccess,
  deleteDocOrgAccess,
  docByUri,
  docsFor,
  getDb,
  repliesForDoc,
  upsertDoc,
  upsertDocAccess,
  upsertDocOrgAccess,
  type DocSummary,
} from "../db";
import {
  clearSession,
  createDocSpace,
  docRole,
  fetchOrgName,
  listMyOrgIds,
  deleteUserGrant,
  orgMembersSpaceUri,
  requireSession,
  setCurrentOrg,
  type DocRole,
} from "./functions.server";
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
} from "./comments.server";
import { SapClient, startLogin } from "./sapClient";

// Every export below is a createServerFn wrapper — safe to statically
// import from any client-reachable file (route components included), per
// TanStack Start's server-functions guide: the build replaces each of
// these with an RPC stub in the client bundle, and the actual handler body
// (plus whatever it imports, e.g. functions.server.ts's server-only code)
// never reaches the browser.

// getCaller resolves the logged-in member's DID, redirecting to /login when
// there isn't one — the one auth check every route/mutation below shares.
export const getCaller = createServerFn({ method: "GET" }).handler(async () =>
  requireSession(),
);

// signOut clears the session cookie. The session cookie is httpOnly, so
// the client can't drop it itself — it has to go through the server.
export const signOut = createServerFn({ method: "POST" }).handler(async () => {
  await clearSession();
});

export const createDoc = createServerFn({ method: "POST" }).handler(
  async (): Promise<{ docId: string; uri: string }> => {
    const { did, currentOrg } = await requireSession();
    const client = new SapClient(env, did);

    const { uri, ownerDid } = await createDocSpace(client, did, currentOrg);

    // An org doc space is created with no opensocial access roles (see
    // createDocSpace), so the member who just created it holds nothing on
    // it — not even enough for the trackSpace call below, which fails its
    // space-credential check as UserNotAuthorized. Grant them manager with
    // the org's own credentials (the org owns the space, so it always
    // qualifies — see managementClient) before anything else touches it.
    if (currentOrg) {
      await managementClient(did, currentOrg).call(
        "network.habitat.relationship.setUserRelation",
        "POST",
        { subject: did, relation: "manager", space: uri },
      );
    }

    // sap has no way to discover this space on its own until the member's
    // next session crawl — tell it explicitly so DocSync's outbox consumer
    // actually receives events for edits to it.
    await client.trackSpace(uri);

    // Create the companion comments space up front rather than on the
    // first comment: a commenter is someone granted writer on it (see
    // shareDoc), and you can't grant a role on a space that doesn't exist
    // yet — so a doc has to be shareable-as-commenter from the moment it
    // is created, not from the moment someone happens to comment.
    await ensureCommentsSpace(client, uri, { ownerDid, isOrg: !!currentOrg });

    // docId is the doc's full space URI, not just its trailing skey: a
    // space's skey alone doesn't say which host/repo it lives under, so a
    // bare skey can't be resolved back to a space without already having it
    // in this instance's DocStore — which breaks for a doc shared with a
    // member whose chalk instance never created or synced it. The full URI
    // is self-describing.
    const docId = uri;

    const db = getDb(env);
    await upsertDoc(db, { spaceUri: uri, docId, ownerDid, title: "Untitled" });

    // Record the creator's own grant locally now rather than waiting on the
    // outbox webhook to sync the matching userRelation record back —
    // without this, the doc the creator just made is hidden from their own
    // listing until that async round-trip lands. Personal docs get owner
    // (simplespace.createSpace grants it); org docs get the manager grant
    // made just above, and stay invisible to the rest of the org until
    // they're shared with it (docsFor lists those from doc_org_access).
    await upsertDocAccess(db, {
      uri,
      spaceUri: uri,
      subjectDid: did,
      relation: currentOrg ? "manager" : "owner",
    });

    // Record the room's identity now, so the owner-republish alarm knows the
    // owner before the webhook (src/server/webhook.ts) delivers it.
    await env.DOC.get(env.DOC.idFromName(uri)).seedIdentity({
      spaceUri: uri,
      ownerDid,
    });

    return { docId, uri };
  },
);

export const listDocs = createServerFn({ method: "GET" }).handler(
  async (): Promise<DocSummary[]> => {
    const { did, currentOrg } = await requireSession();
    return docsFor(getDb(env), did, currentOrg);
  },
);

// getCurrentOrg resolves the member's currently-selected org, if any — used
// by the sidebar to show which mode (Personal, or which org) is active. Name
// is null if the caller isn't a member of the org (or the read fails) — the
// sidebar falls back to showing the raw DID.
export const getCurrentOrg = createServerFn({ method: "GET" }).handler(
  async (): Promise<{ did: string; name: string | null } | undefined> => {
    const { did, currentOrg } = await requireSession();
    if (!currentOrg) return undefined;
    const client = new SapClient(env, did);
    return {
      did: currentOrg,
      name: await fetchOrgName(client, currentOrg as DidString),
    };
  },
);

export interface OrgOption {
  did: string;
  name: string | null;
  // Whether any member has already completed the OAuth admin-approval flow
  // for this org before (see upsertConnectedOrg) — org-wide, not
  // per-member, so the /orgs picker distinguishes orgs that just need
  // switching-to from ones that still need the full connect flow.
  connected: boolean;
}

// listMyOrgs lists every org the member belongs to, with a best-effort
// display name (null if the read failed — the org-picker falls back to
// showing the raw DID). Already-connected orgs reuse the name recorded at
// connect time (see connectedOrgNames) instead of re-fetching it from the
// org's PDS on every call; only orgs nobody has connected yet need that
// fetch.
export const listMyOrgs = createServerFn({ method: "GET" }).handler(
  async (): Promise<OrgOption[]> => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
    const orgIds = await listMyOrgIds(client);
    const names = await connectedOrgNames(getDb(env), orgIds);
    return Promise.all(
      orgIds.map(async (orgDid) => {
        const connectedName = names.get(orgDid);
        return {
          did: orgDid,
          name: connectedName ?? (await fetchOrgName(client, orgDid)),
          connected: connectedName !== undefined,
        };
      }),
    );
  },
);

// switchOrg sets an already-connected org as the member's active org,
// without redoing the OAuth admin-approval round-trip startOrgConnect
// requires for a first-time connection.
export const switchOrg = createServerFn({ method: "POST" })
  .validator((input: { orgDid: string }) => input)
  .handler(async ({ data }) => {
    await requireSession();
    await setCurrentOrg(data.orgDid);
  });

// switchToPersonal takes the member out of org mode, back to acting on
// their own personal docs.
export const switchToPersonal = createServerFn({ method: "POST" }).handler(
  async () => {
    await requireSession();
    await setCurrentOrg(undefined);
  },
);

// startOrgConnect asks sap to begin the opensocial admin sign-in flow for
// orgDid, telling it to redirect the browser back to chalk's
// /session/org-callback (with the resolved DID — always orgDid itself,
// since handleAddSession resolves whatever identifier it's given) once
// that flow completes. Returns the URL the browser should be sent to next.
// Mirrors startLogin (sapClient.ts) exactly, but with a DID instead of a
// handle and a different return_to.
export const startOrgConnect = createServerFn({ method: "POST" })
  .validator((input: { orgDid: string }) => input)
  .handler(async ({ data }): Promise<{ redirectUrl: string }> => {
    await requireSession();
    return {
      redirectUrl: await startLogin(env, data.orgDid, "/session/org-callback"),
    };
  });

// A userRelation record as network.habitat.relationship.listRelations
// returns it — only the fields sharing.ts actually reads.
interface UserRelationView {
  uri: string;
  subject: string;
  relation: string;
}

// A spaceRelation record as network.habitat.relationship.listRelations
// returns it — only the fields the org-sharing functions below read.
interface SpaceRelationView {
  uri: string;
  subject: string;
  relation: string;
}

// managementClient returns the SapClient to use for calls that manage a
// doc's sharing (list/grant/revoke access): in org mode this is the org's
// own OAuth session (sap already tracks one, established when the org was
// connected — see startOrgConnect), not the calling member's. The org owns
// its doc spaces outright, so it always passes pear's manager check
// (CheckUserHasSpaceRole's implicit-owner rule) — unlike an individual
// member, who may hold no relation on the doc at all until someone with
// manager access grants them one. This sidesteps that bootstrapping problem
// entirely: sharing an org doc never depends on the acting member's own
// grant. In personal mode there's no org session, so this is just the
// member's own client, same as everywhere else.
function managementClient(
  did: string,
  currentOrg: string | undefined,
): SapClient {
  return new SapClient(env, currentOrg ?? did);
}

// A doc grantee holds one of three tiers, which map onto the relations
// network.habitat.relationship actually stores — and, for a commenter,
// onto a different space entirely:
//
//   editor    -> manager on the doc space
//   commenter -> writer  on the comments space
//   viewer    -> reader  on the doc space
//
// Editors get "manager" (not just "writer") so they can share the doc
// themselves — pear's setUserRelation requires manager — and manager
// implies writer, so this doesn't change what an editor can do to the
// doc's content.
//
// A commenter is granted on the comments space alone. They still get to
// read the doc, because the comments space's writers are readers of the
// doc space (see SPACE_RELATIONS) — one grant, not two to keep in sync,
// and one record to delete when access is revoked.
const ROLE_TO_GRANT: Record<
  DocRole,
  { relation: "manager" | "writer" | "reader"; onComments: boolean }
> = {
  editor: { relation: "manager", onComments: false },
  commenter: { relation: "writer", onComments: true },
  viewer: { relation: "reader", onComments: false },
};

// grantSpaces returns the two spaces a doc's grants can live on — its own
// and its comments space — as the pair every sharing call has to consider.
// The comments space entry is undefined only for a malformed docId, which
// commentsSpaceUri rejects.
function grantSpaces(docId: string): { space: string; onComments: boolean }[] {
  const commentsSpace = commentsSpaceUri(docId);
  return [
    { space: docId, onComments: false },
    ...(commentsSpace ? [{ space: commentsSpace, onComments: true }] : []),
  ];
}

// relationToRole maps a stored relation back to the tier it represents.
// The same relation means different things on the two spaces — "writer"
// on the comments space is a commenter, while on the doc space it would
// be an editor — so the space it was found on is part of the answer.
function relationToRole(
  relation: string,
  onComments: boolean,
): DocRole | undefined {
  if (onComments) return relation === "writer" ? "commenter" : undefined;
  if (relation === "manager" || relation === "writer") return "editor";
  if (relation === "reader") return "viewer";
  return undefined;
}

// listDocAccess returns every user with a direct grant on the doc and the
// tier it gives them — the "people with access" list a share dialog shows.
// Both spaces are queried, since a commenter's grant is a record on the
// comments space rather than the doc's own (see ROLE_TO_GRANT).
//
// Only user grants (subjectType "user"), not space/group usersets:
// chalk's sharing is user-to-user for now. That also keeps the spaces'
// own inheritance relations out of the list — they're space usersets, not
// user grants, so a doc editor never shows up here twice.
export const listDocAccess = createServerFn({ method: "GET" })
  .validator((input: { docId: string }) => input)
  .handler(async ({ data }): Promise<{ did: string; role: DocRole }[]> => {
    const { did, currentOrg } = await requireSession();
    const client = managementClient(did, currentOrg);
    const perSpace = await Promise.all(
      grantSpaces(data.docId).map(async ({ space, onComments }) => {
        const { relations } = await client.call<{
          relations: UserRelationView[];
        }>("network.habitat.relationship.listRelations", "GET", {
          space,
          subjectType: "user",
        });
        return relations.flatMap((r) => {
          const role = relationToRole(r.relation, onComments);
          return role ? [{ did: r.subject, role }] : [];
        });
      }),
    );
    return perSpace.flat();
  });

// shareDoc grants a user access to a doc as an editor, a commenter or a
// viewer. Requires the caller to already hold manager on the space being
// granted on (pear enforces this; a non-manager's setUserRelation call
// fails there, not here) — which for a commenter is the comments space,
// where doc-space managers hold manager through the inheritance
// SPACE_RELATIONS sets up, so any editor can add one. In org mode this goes
// through managementClient's org session instead, which always qualifies.
export const shareDoc = createServerFn({ method: "POST" })
  .validator(
    (input: { docId: string; subjectDid: string; role: DocRole }) => input,
  )
  .handler(async ({ data }) => {
    const { did, currentOrg } = await requireSession();
    const client = managementClient(did, currentOrg);
    const { relation, onComments } = ROLE_TO_GRANT[data.role];
    const space = onComments ? commentsSpaceUri(data.docId) : data.docId;
    if (!space) throw new Error("invalid docId");

    const { uri } = await client.call<{ uri: string }>(
      "network.habitat.relationship.setUserRelation",
      "POST",
      { subject: data.subjectDid, relation, space },
    );

    // Re-sharing replaces whatever the subject held before. Within one
    // space that's automatic — pear's SetUserRelation keys the record by
    // subject DID and drops the other roles' tuples — but commenter lives
    // on the comments space while editor and viewer live on the doc
    // space, so a change across that line has to clear the old grant
    // explicitly. Left alone it would keep granting the old role: a
    // commenter demoted to viewer would still hold writer on the comments
    // space, which is the whole of what makes someone a commenter.
    //
    // The new grant is written first so the subject is never briefly
    // without access, and a failure here surfaces rather than being
    // swallowed — a half-applied role change is worth an error.
    const staleSpace = grantSpaces(data.docId).find((s) => s.space !== space);
    const staleUri = staleSpace
      ? await deleteUserGrant(client, staleSpace.space, data.subjectDid)
      : undefined;

    // Update local doc_access immediately rather than waiting on the
    // outbox webhook to sync these same records back — without this, the
    // newly-shared user's own docsFor query won't show the doc
    // until that async round-trip lands. The webhook's own upsert/delete
    // is a no-op once this has already landed (same uri).
    //
    // Keyed by the *doc* space even for a commenter, whose grant record
    // lives on the comments space: doc_access exists to answer "which docs
    // can this subject see", and docsFor joins it against the doc.
    // The outbox handler maps the same record the same way.
    const db = getDb(env);
    if (staleUri) await deleteDocAccess(db, staleUri);
    await upsertDocAccess(db, {
      uri,
      spaceUri: data.docId,
      subjectDid: data.subjectDid,
      relation,
    });
  });

// getDocRole resolves the caller's own tier on the doc, so the client can
// keep the editor read-only for a commenter or viewer and hide the
// comment box from a viewer. See docRole's comment: this is a UX signal,
// not the access gate itself.
export const getDocRole = createServerFn({ method: "GET" })
  .validator((input: { docId: string }) => input)
  .handler(async ({ data }): Promise<DocRole | null> => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
    return docRole(client, did, data.docId);
  });

// getDocInitialState returns the doc's current Yjs state so the route
// loader can seed useYDoc's Y.Doc before the editor ever renders, instead of
// it starting empty and only filling in once the WebSocket connects.
// DocRoom itself has no ACL (see hasDocAccess's comment), so this is the
// one place this path checks access before returning content.
export const getDocInitialState = createServerFn({ method: "GET" })
  .validator((input: { docId: string }) => input)
  .handler(async ({ data }): Promise<Uint8Array> => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
    if (!(await docRole(client, did, data.docId))) {
      throw new Error("forbidden");
    }
    return env.DOC.get(env.DOC.idFromName(data.docId)).snapshot();
  });

// revokeDocAccess removes a user's grant (see deleteUserGrant, which does
// the record lookup the delete needs).
//
// Both spaces are swept, since a commenter's grant record lives on the
// comments space rather than the doc's own (see ROLE_TO_GRANT). A user
// holds a grant on one or the other, never both, but revoking deletes
// whatever it finds rather than stopping at the first: leaving a stale
// grant behind on the other space would silently keep access alive.
export const revokeDocAccess = createServerFn({ method: "POST" })
  .validator((input: { docId: string; subjectDid: string }) => input)
  .handler(async ({ data }) => {
    const { did, currentOrg } = await requireSession();
    const client = managementClient(did, currentOrg);
    for (const { space } of grantSpaces(data.docId)) {
      const uri = await deleteUserGrant(client, space, data.subjectDid);
      if (!uri) continue;

      // Remove local doc_access immediately rather than waiting on the
      // outbox webhook to sync this same tombstone back — without this,
      // the revoked user keeps seeing the doc in their own listDocs until
      // that async round-trip lands.
      await deleteDocAccess(getDb(env), uri);
    }
  });

export type {
  CommentReplyView,
  CommentView,
  StrongRef,
} from "./comments.server";

// A CommentsPayload bundles a doc's comment threads — each root comment
// with its replies nested directly on it (see CommentView in
// comments.server.ts) rather than two parallel lists the client would have
// to re-join itself. The underlying data still comes from two separate
// tables/record kinds (a root comment carries the thread's CRDT anchor; a
// reply just references its root by strongRef — see each lexicon's comment
// for why the anchor isn't repeated on both); this is the one round-trip
// and one join that produces the client's view of it.
//
// A reply whose root comment has been deleted (deleting a thread's root
// doesn't cascade-delete its replies) has nowhere to nest and is simply
// omitted — without a root there's no anchor to show it against anyway.
export interface CommentsPayload {
  comments: CommentView[];
}

// listComments returns a doc's comment threads from chalk's own D1 mirror
// rather than reading the comments space on every call: the outbox already
// delivers every comment/reply record written anywhere in the space (see
// outbox.ts), and the mirror is what makes this one indexed lookup instead
// of a listRecords fan-out across every commenter's repo.
//
// The caller still has to hold reader on the *doc* for this to return
// anything — the comments space inherits its readers from the doc space
// (see ensureCommentsSpace), so that check is the same question as "may
// this caller read the comments", asked against the space chalk already
// has a role check for.
export const listComments = createServerFn({ method: "GET" })
  .validator((input: { docId: string }) => input)
  .handler(async ({ data }): Promise<CommentsPayload> => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
    if (!(await docRole(client, did, data.docId))) {
      return { comments: [] };
    }
    const db = getDb(env);
    const [comments, replies] = await Promise.all([
      commentsForDoc(db, data.docId),
      repliesForDoc(db, data.docId),
    ]);

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
    };
  });

// createComment starts a new thread by writing a root comment into the
// doc's comments space (see writeComment in comments.server.ts), creating
// that space and its inheritance from the doc space on first use.
// anchorStart/anchorEnd are Yjs relative positions the client computed
// from its own live Y.Doc before calling this — the server has no editor
// state of its own to derive them from.
//
// Requires editor or commenter — the two tiers that hold writer on the
// comments space (an editor through the doc space's inheritance, a
// commenter by direct grant). A viewer's putRecord would be rejected by
// pear anyway; checking here just turns that into a clear error instead
// of a proxied 403.
export const createComment = createServerFn({ method: "POST" })
  .validator(
    (input: {
      docId: string;
      body: string;
      anchorStart: string;
      anchorEnd: string;
      quotedText?: string;
    }) => input,
  )
  .handler(async ({ data }): Promise<CommentView> => {
    const { did, currentOrg } = await requireSession();
    const client = new SapClient(env, did);
    if (!canComment(await docRole(client, did, data.docId))) {
      throw new Error("forbidden");
    }
    const doc = await docByUri(getDb(env), data.docId);
    const ownerDid = doc?.ownerDid ?? did;
    return writeComment(client, getDb(env), did, data.docId, {
      body: data.body,
      anchorStart: data.anchorStart,
      anchorEnd: data.anchorEnd,
      quotedText: data.quotedText,
      ownerDid,
      // docs no longer records whether a doc is an org's; a doc owned by the
      // org the caller is currently acting as is the org case.
      isOrg: currentOrg !== undefined && ownerDid === currentOrg,
    });
  });

// canComment reports whether a role may write comments — editors and
// commenters, but not viewers (and not a null role: no access at all).
// The single place createComment/createReply agree on what "may comment"
// means.
function canComment(role: DocRole | null): boolean {
  return role === "editor" || role === "commenter";
}

// createReply adds a reply to an existing thread — see writeReply in
// comments.server.ts. `comment` is the strongRef to the thread's root, as
// returned by listComments/createComment. Same tier requirement as
// createComment.
export const createReply = createServerFn({ method: "POST" })
  .validator(
    (input: { docId: string; comment: StrongRef; body: string }) => input,
  )
  .handler(async ({ data }): Promise<CommentReplyView> => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
    if (!canComment(await docRole(client, did, data.docId))) {
      throw new Error("forbidden");
    }
    return writeReply(client, getDb(env), did, data.docId, {
      comment: data.comment,
      body: data.body,
    });
  });

// deleteCommentFn removes one root comment — see removeComment in
// comments.server.ts, which also enforces that only the comment's own
// author can delete it.
export const deleteCommentFn = createServerFn({ method: "POST" })
  .validator((input: { docId: string; uri: string }) => input)
  .handler(async ({ data }) => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
    await removeComment(client, getDb(env), did, data.uri);
  });

// deleteReplyFn removes one reply — see removeReply in comments.server.ts.
export const deleteReplyFn = createServerFn({ method: "POST" })
  .validator((input: { docId: string; uri: string }) => input)
  .handler(async ({ data }) => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
    await removeReply(client, getDb(env), did, data.uri);
  });

// The org-wide grant maps to "reader"/"writer" (not "manager", used for a
// per-person editor) — every member reshaing the doc org-wide would be too
// permissive for a grant this broad.
const ORG_ROLE_TO_RELATION: Record<"editor" | "viewer", "writer" | "reader"> = {
  editor: "writer",
  viewer: "reader",
};

// getOrgSpaceRelation looks up the doc's spaceRelation naming the caller's
// org's members space as its subject, if any — the single record that
// backs the "share with everyone at <org>" option.
async function getOrgSpaceRelation(
  client: SapClient,
  docId: string,
  currentOrg: string,
): Promise<SpaceRelationView | undefined> {
  const { relations } = await client.call<{ relations: SpaceRelationView[] }>(
    "network.habitat.relationship.listRelations",
    "GET",
    { space: docId, subjectType: "space" },
  );
  const membersSpace = orgMembersSpaceUri(currentOrg);
  return relations.find((r) => r.subject === membersSpace);
}

// getDocOrgAccess resolves whether (and how) a doc is currently shared with
// every member of the caller's org, for the org-sharing control to show its
// current state. Returns null outside org mode, or when nothing is shared.
export const getDocOrgAccess = createServerFn({ method: "GET" })
  .validator((input: { docId: string }) => input)
  .handler(async ({ data }): Promise<"editor" | "viewer" | null> => {
    const { currentOrg } = await requireSession();
    if (!currentOrg) return null;
    const client = new SapClient(env, currentOrg);
    const relation = await getOrgSpaceRelation(client, data.docId, currentOrg);
    if (!relation) return null;
    return relation.relation === "writer" ? "editor" : "viewer";
  });

// shareDocWithOrg grants every member of the caller's current org access to
// a doc, via a single spaceRelation naming the org's own
// community.opensocial.members space as its subject (see
// orgMembersSpaceUri) — this is what the share dialog's "entire org" option
// calls. Always org-mode only; see managementClient's comment for why this
// uses the org's own session rather than the calling member's.
export const shareDocWithOrg = createServerFn({ method: "POST" })
  .validator((input: { docId: string; role: "editor" | "viewer" }) => input)
  .handler(async ({ data }) => {
    const { currentOrg } = await requireSession();
    if (!currentOrg) throw new Error("not acting as an org");
    // Called with the org's own OAuth session (not the member's): the org
    // owns the doc space outright, so this always passes pear's manager
    // check regardless of what the acting member personally holds — see
    // managementClient's comment above for why that matters.
    const client = new SapClient(env, currentOrg);
    const { uri } = await client.call<{ uri: string }>(
      "network.habitat.relationship.setSpaceRelation",
      "POST",
      {
        subject: orgMembersSpaceUri(currentOrg),
        subjectRole: "reader",
        relation: ORG_ROLE_TO_RELATION[data.role],
        space: data.docId,
      },
    );

    // Record the org-wide grant locally now rather than waiting on the
    // outbox webhook to sync this same spaceRelation back — without this,
    // the doc doesn't appear in other members' listDocs until that async
    // round-trip lands. outbox.ts's handleSpaceRelation is a no-op once
    // this has already landed (same uri).
    await upsertDocOrgAccess(getDb(env), {
      uri,
      spaceUri: data.docId,
      orgDid: currentOrg,
      relation: ORG_ROLE_TO_RELATION[data.role],
    });
  });

// revokeDocOrgAccess removes the doc's org-wide grant, if any — the share
// dialog's "not shared with org" option.
export const revokeDocOrgAccess = createServerFn({ method: "POST" })
  .validator((input: { docId: string }) => input)
  .handler(async ({ data }) => {
    const { currentOrg } = await requireSession();
    if (!currentOrg) return;
    const client = new SapClient(env, currentOrg);
    const relation = await getOrgSpaceRelation(client, data.docId, currentOrg);
    if (!relation) return;
    await client.call("network.habitat.relationship.deleteRelation", "POST", {
      uri: relation.uri,
    });

    // Drop the local row immediately, for the same reason shareDocWithOrg
    // writes it immediately: otherwise the doc keeps showing up in every
    // org member's listDocs until the outbox tombstone lands.
    await deleteDocOrgAccess(getDb(env), relation.uri);
  });
