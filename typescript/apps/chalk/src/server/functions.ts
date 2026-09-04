import { createServerFn } from "@tanstack/react-start";
import { env } from "cloudflare:workers";
import {
  connectedOrgNames,
  deleteDocAccess,
  docsForAccessor,
  docsForOrg,
  getDb,
  upsertDoc,
  upsertDocAccess,
  type DocSummary,
} from "../db";
import {
  clearSession,
  createDocSpace,
  docRole,
  fetchOrgName,
  listMyOrgIds,
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

    // Personal docs grant the owner local doc_access now rather than
    // waiting on the outbox webhook to sync their own userRelation record
    // back — without this, docsForAccessor's inner join hides the doc the
    // owner just created until that async round-trip lands. Org docs have
    // no doc_access rows at all: access is org-wide via the space's own
    // community.opensocial.access record (see createDocSpace), not a
    // per-user grant.
    if (!isOrg) {
      await upsertDocAccess(db, {
        uri,
        spaceUri: uri,
        subjectDid: did,
        relation: "owner",
      });
    }

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
    return currentOrg ? docsForOrg(db, currentOrg) : docsForAccessor(db, did);
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
      const { did } = await requireSession();
      const client = new SapClient(env, did);
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
// Requires the caller to already hold manager (pear enforces this; a
// non-manager's setUserRelation call fails there, not here).
export const shareDoc = createServerFn({ method: "POST" })
  .validator(
    (input: { docId: string; subjectDid: string; role: "editor" | "viewer" }) =>
      input,
  )
  .handler(async ({ data }) => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
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
    const { did } = await requireSession();
    const client = new SapClient(env, did);
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
