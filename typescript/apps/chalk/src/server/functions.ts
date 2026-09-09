import { createServerFn } from "@tanstack/react-start";
import { env } from "cloudflare:workers";
import {
  connectedOrgNames,
  deleteDocAccess,
  deleteDocOrgAccess,
  docsForAccessor,
  docsForOrg,
  getDb,
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
  orgMembersSpaceUri,
  requireSession,
  setCurrentOrg,
} from "./functions.server";
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

    const { uri, ownerDid, isOrg } = await createDocSpace(
      client,
      did,
      currentOrg,
    );

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

    // docId is the doc's full space URI, not just its trailing skey: a
    // space's skey alone doesn't say which host/repo it lives under, so a
    // bare skey can't be resolved back to a space without already having it
    // in this instance's DocStore — which breaks for a doc shared with a
    // member whose chalk instance never created or synced it. The full URI
    // is self-describing.
    const docId = uri;

    const db = getDb(env);
    await upsertDoc(db, {
      spaceUri: uri,
      docId,
      ownerDid,
      title: "Untitled",
      isOrg,
    });

    // Record the creator's own grant locally now rather than waiting on the
    // outbox webhook to sync the matching userRelation record back —
    // without this, the doc the creator just made is hidden from their own
    // listing until that async round-trip lands. Personal docs get owner
    // (simplespace.createSpace grants it); org docs get the manager grant
    // made just above, and stay invisible to the rest of the org until
    // they're shared with it (docsForOrg lists from doc_org_access).
    await upsertDocAccess(db, {
      uri,
      spaceUri: uri,
      subjectDid: did,
      relation: isOrg ? "manager" : "owner",
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
    const db = getDb(env);
    return currentOrg
      ? docsForOrg(db, currentOrg, did)
      : docsForAccessor(db, did);
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
    return { did: currentOrg, name: await fetchOrgName(client, currentOrg) };
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

// listDocAccess returns every user with a direct grant on the doc, and
// their relation — the "people with access" list a share dialog shows.
// Only user grants (subjectType "user"), not space/group usersets:
// chalk's sharing is user-to-user for now.
export const listDocAccess = createServerFn({ method: "GET" })
  .validator((input: { docId: string }) => input)
  .handler(
    async ({
      data,
    }): Promise<{ did: string; relation: "manager" | "reader" }[]> => {
      const { did, currentOrg } = await requireSession();
      const client = managementClient(did, currentOrg);
      const { relations } = await client.call<{
        relations: UserRelationView[];
      }>("network.habitat.relationship.listRelations", "GET", {
        space: data.docId,
        subjectType: "user",
      });
      return relations.map((r) => ({
        did: r.subject,
        relation: r.relation as "manager" | "reader",
      }));
    },
  );

// A doc grantee is either an editor (can edit the doc) or a viewer
// (read-only), which map to the "manager"/"reader" relations
// network.habitat.relationship actually stores. Editors get "manager"
// (not just "writer") so they can share the doc themselves — pear's
// setUserRelation requires manager — and manager implies writer, so this
// doesn't change what an editor can do to the doc's content.
const ROLE_TO_RELATION: Record<"editor" | "viewer", "manager" | "reader"> = {
  editor: "manager",
  viewer: "reader",
};

// shareDoc grants a user access to a doc as either an editor or a viewer.
// In personal mode the caller must already hold manager themselves (pear
// enforces this; a non-manager's setUserRelation call fails there, not
// here). In org mode this goes through managementClient's org session
// instead, which always qualifies.
export const shareDoc = createServerFn({ method: "POST" })
  .validator(
    (input: { docId: string; subjectDid: string; role: "editor" | "viewer" }) =>
      input,
  )
  .handler(async ({ data }) => {
    const { did, currentOrg } = await requireSession();
    const client = managementClient(did, currentOrg);
    const { uri } = await client.call<{ uri: string }>(
      "network.habitat.relationship.setUserRelation",
      "POST",
      {
        subject: data.subjectDid,
        relation: ROLE_TO_RELATION[data.role],
        space: data.docId,
      },
    );

    // Grant local doc_access immediately rather than waiting on the outbox
    // webhook to sync this same userRelation record back — without this,
    // the newly-shared user's own docsForAccessor query won't show the doc
    // until that async round-trip lands. The webhook's own upsertDocAccess
    // call is a no-op once this has already landed (same uri).
    await upsertDocAccess(getDb(env), {
      uri,
      spaceUri: data.docId,
      subjectDid: data.subjectDid,
      relation: ROLE_TO_RELATION[data.role],
    });
  });

// getDocRole resolves the caller's own role on the doc, so the client can
// keep the editor read-only for a viewer. See docRole's comment: this is
// a UX signal, not the access gate itself.
export const getDocRole = createServerFn({ method: "GET" })
  .validator((input: { docId: string }) => input)
  .handler(async ({ data }): Promise<"editor" | "viewer" | null> => {
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

// revokeDocAccess removes a user's grant. deleteRelation takes the relation
// record's own URI, not a (did, space) pair, so this looks that URI up via
// the same listRelations query listDocAccess uses, filtered to the one
// subject — no separate index of grant URIs needs to be kept anywhere.
export const revokeDocAccess = createServerFn({ method: "POST" })
  .validator((input: { docId: string; subjectDid: string }) => input)
  .handler(async ({ data }) => {
    const { did, currentOrg } = await requireSession();
    const client = managementClient(did, currentOrg);
    const { relations } = await client.call<{ relations: UserRelationView[] }>(
      "network.habitat.relationship.listRelations",
      "GET",
      { space: data.docId, subjectType: "user", subjectDid: data.subjectDid },
    );
    const relation = relations[0];
    if (!relation) return;
    await client.call("network.habitat.relationship.deleteRelation", "POST", {
      uri: relation.uri,
    });

    // Remove local doc_access immediately rather than waiting on the
    // outbox webhook to sync this same tombstone back — without this, the
    // revoked user keeps seeing the doc in their own listDocs until that
    // async round-trip lands.
    await deleteDocAccess(getDb(env), relation.uri);
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
