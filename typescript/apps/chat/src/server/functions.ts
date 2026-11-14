import { createServerFn } from "@tanstack/react-start";
import { ensureValidDid, type DidString } from "@atproto/syntax";
import { env } from "cloudflare:workers";
import {
  connectedOrgNames,
  getDb,
  isConnectedOrg,
  upsertConnectedOrg,
} from "../db";
import * as chatFunctions from "@/habitat/functions";
import type { ChannelView, MessageView } from "@/habitat/functions";
import {
  memberSession,
  requireMember,
  requireOrg,
  resolveHandle,
  selectOrg,
} from "./functions.server";
import { fetchOrgName, listMyOrgIds, MEMBER_ROLE } from "./habitat";
import { createOAuthClient, OAUTH_SCOPE } from "./oauth";
import { useAppSession } from "./session";
import { chatRuntime } from "./runtime";
import { connectOrg, createChannel as createOrgChannel } from "./sync";

// Every export below is a createServerFn wrapper, safe to import from route
// components: the client bundle only gets RPC stubs (see chalk's
// functions.ts for the longer version of this note).

// OAuthAppState rides through the OAuth round-trip in the `state` param and
// tells /oauth/callback which flow just finished.
export type OAuthAppState =
  { kind: "member" } | { kind: "org"; orgDid: string; memberDid: string };

// completeOAuth finishes whichever OAuth flow just redirected back to
// /oauth/callback, and says where to send the browser next.
//
// - A member sign-in sets the session cookie and goes to the org picker.
// - An org connect checks the session that came back really is the org's
//   (and that the member who started it is still the one signed in), then
//   records the org as connected, starts syncing its existing channels, and
//   makes it the member's working org.
export const completeOAuth = createServerFn({ method: "POST" })
  .validator((input: { params: Record<string, string> }) => input)
  .handler(async ({ data }): Promise<{ to: "/" | "/orgs" | "/login" }> => {
    const oauth = createOAuthClient(env);
    const { session: oauthSession, state } = await oauth.callback(
      new URLSearchParams(data.params),
    );
    const appState: OAuthAppState = state
      ? (JSON.parse(state) as OAuthAppState)
      : { kind: "member" };
    const session = await useAppSession();

    if (appState.kind === "member") {
      await session.update({ did: oauthSession.did, currentOrg: undefined });
      return { to: "/orgs" };
    }

    if (session.data.did !== appState.memberDid) return { to: "/login" };
    if (oauthSession.did !== appState.orgDid) {
      throw new Error(
        "Signed in as a different identity than the organization being connected",
      );
    }
    const orgDid = oauthSession.did;
    const name = (await fetchOrgName(oauthSession, orgDid)) ?? orgDid;
    await upsertConnectedOrg(getDb(env), {
      orgDid,
      name,
      connectedBy: appState.memberDid,
    });
    await connectOrg(env, orgDid);
    await selectOrg(env, appState.memberDid as DidString, orgDid);
    return { to: "/" };
  });

export interface Member {
  did: string;
  handle: string | null;
}

export const getMember = createServerFn({ method: "GET" }).handler(
  async (): Promise<Member> => {
    const { did } = await requireMember();
    return { did, handle: await resolveHandle(env, did) };
  },
);

// startLogin begins the member's OAuth sign-in, returning the authorization
// server URL the browser should go to next.
export const startLogin = createServerFn({ method: "POST" })
  .validator((input: { handle: string }) => input)
  .handler(async ({ data }) => {
    const state: OAuthAppState = { kind: "member" };
    const url = await createOAuthClient(env).authorize(data.handle, {
      scope: OAUTH_SCOPE,
      state: JSON.stringify(state),
    });
    return { redirectUrl: url.toString() };
  });

export const signOut = createServerFn({ method: "POST" }).handler(async () => {
  const session = await useAppSession();
  const did = session.data.did;
  await session.clear();
  if (did) {
    // Best-effort: forget the member's tokens too. Org sessions are left
    // alone — they belong to the org, not to whoever connected it.
    try {
      await createOAuthClient(env).revoke(did);
    } catch (err) {
      console.error("[chat] revoking member session", err);
    }
  }
});

export interface OrgOption {
  did: string;
  name: string | null;
  // Whether this deployment already holds the org's own session. Until an
  // admin connects it, nobody can create or sync the org's channels.
  connected: boolean;
}

// listMyOrgs lists every org the member belongs to. Connected orgs reuse
// the name cached at connect time; the rest are read from the org's
// profile (null if that fails — the picker falls back to the DID).
export const listMyOrgs = createServerFn({ method: "GET" }).handler(
  async (): Promise<OrgOption[]> => {
    const { did } = await requireMember();
    const agent = await memberSession(env, did);
    const orgIds = await listMyOrgIds(agent);
    const names = await connectedOrgNames(getDb(env), orgIds);
    return Promise.all(
      orgIds.map(async (orgDid) => {
        const cached = names.get(orgDid);
        return {
          did: orgDid,
          name: cached ?? (await fetchOrgName(agent, orgDid)),
          connected: cached !== undefined,
        };
      }),
    );
  },
);

export const getCurrentOrg = createServerFn({ method: "GET" }).handler(
  async (): Promise<{ did: string; name: string | null } | null> => {
    const { currentOrg } = await requireMember();
    if (!currentOrg) return null;
    const names = await connectedOrgNames(getDb(env), [currentOrg]);
    return { did: currentOrg, name: names.get(currentOrg) ?? null };
  },
);

// switchOrg selects an already-connected org.
export const switchOrg = createServerFn({ method: "POST" })
  .validator((input: { orgDid: string }) => {
    ensureValidDid(input.orgDid);
    return { orgDid: input.orgDid as DidString };
  })
  .handler(async ({ data }) => {
    const { did } = await requireMember();
    if (!(await isConnectedOrg(getDb(env), data.orgDid))) {
      throw new Error("This organization hasn't been connected to Chat yet");
    }
    await selectOrg(env, did, data.orgDid);
  });

// startOrgConnect begins the OAuth flow for the org's own identity. pear
// shows the org's admin-approval page for it (only an admin of the org can
// complete it), then redirects back to /oauth/callback, which stores the
// org's session and starts syncing it.
export const startOrgConnect = createServerFn({ method: "POST" })
  .validator((input: { orgDid: string }) => {
    ensureValidDid(input.orgDid);
    return input;
  })
  .handler(async ({ data }) => {
    const { did } = await requireMember();
    const state: OAuthAppState = {
      kind: "org",
      orgDid: data.orgDid,
      memberDid: did,
    };
    const url = await createOAuthClient(env).authorize(data.orgDid, {
      scope: OAUTH_SCOPE,
      state: JSON.stringify(state),
    });
    return { redirectUrl: url.toString() };
  });

// --- Chat itself -------------------------------------------------------------
//
// These are thin: each resolves the signed-in member and runs one of the
// Habitat app's functions (src/habitat/functions.ts) as them. The runtime
// restricts every read to channels that member can read.

// memberRuntime is the runtime for a request: mutations write as the
// viewer through their own OAuth session.
function memberRuntime() {
  return chatRuntime(env, (viewer) => memberSession(env, viewer as DidString));
}

export interface MessageWithHandle extends MessageView {
  authorHandle: string | null;
}

// listChannels returns the current org's channels the member can read.
export const listChannels = createServerFn({ method: "GET" }).handler(
  async (): Promise<ChannelView[]> => {
    const { did, currentOrg } = await requireOrg();
    return memberRuntime().run(chatFunctions.listChannels, {
      viewer: did,
      args: { org: currentOrg },
    });
  },
);

// listMessages returns a channel's recent messages with the slice version
// they were read at, which the page polls (getVersion) to know when to ask
// again.
export const listMessages = createServerFn({ method: "GET" })
  .validator((input: { space: string }) => input)
  .handler(
    async ({
      data,
    }): Promise<{ messages: MessageWithHandle[]; version: number }> => {
      const { did } = await requireOrg();
      const runtime = memberRuntime();
      const version = await runtime.version();
      const messages = await runtime.run(chatFunctions.listMessages, {
        viewer: did,
        args: { space: data.space },
      });
      const authors = [...new Set(messages.map((m) => m.author))];
      const handles = new Map(
        await Promise.all(
          authors.map(async (d) => [d, await resolveHandle(env, d)] as const),
        ),
      );
      return {
        messages: messages.map((m) => ({
          ...m,
          authorHandle: handles.get(m.author) ?? null,
        })),
        version,
      };
    },
  );

// getVersion is the slice's version counter: it moves whenever any synced
// record or permission changes, so polling it is a cheap way to learn that
// the lists above are stale.
export const getVersion = createServerFn({ method: "GET" }).handler(
  async (): Promise<number> => {
    await requireMember();
    return memberRuntime().version();
  },
);

export const sendMessage = createServerFn({ method: "POST" })
  .validator((input: { space: string; text: string }) => input)
  .handler(async ({ data }) => {
    const { did } = await requireOrg();
    await memberRuntime().run(chatFunctions.sendMessage, {
      viewer: did,
      args: data,
    });
  });

export const deleteMessage = createServerFn({ method: "POST" })
  .validator((input: { space: string; rkey: string }) => input)
  .handler(async ({ data }) => {
    const { did } = await requireOrg();
    await memberRuntime().run(chatFunctions.deleteMessage, {
      viewer: did,
      args: data,
    });
  });

// createChannel creates a channel in the current org, open to every member.
// The space is created with the org's own session, so (like uploading to
// Drop) any member of a connected org can do it.
export const createChannel = createServerFn({ method: "POST" })
  .validator((input: { name: string; topic?: string }) => {
    const name = input.name.trim();
    if (name.length === 0 || name.length > 64) {
      throw new Error("A channel name must be 1 to 64 characters");
    }
    const topic = input.topic?.trim();
    if (topic && topic.length > 256) {
      throw new Error("A channel topic must be at most 256 characters");
    }
    return { name, topic: topic || undefined };
  })
  .handler(async ({ data }) => {
    const { did, currentOrg } = await requireOrg();
    return createOrgChannel(env, {
      orgDid: currentOrg,
      memberDid: did,
      name: data.name,
      topic: data.topic,
      roles: [MEMBER_ROLE],
    });
  });
