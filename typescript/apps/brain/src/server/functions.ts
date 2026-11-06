import { createServerFn } from "@tanstack/react-start";
import { env } from "cloudflare:workers";
import {
  listMcpServers,
  startMcpAuthorization,
  type McpServerWithStatus,
} from "internal/mcp";
import type { DidString } from "@atproto/syntax";
import {
  chatOwner,
  chatsForOwner,
  getDb,
  messagesForChat,
  type ChatSummary,
} from "../db";
import {
  memberSession,
  requireMember,
  resolveHandle,
} from "./functions.server";
import { listLlmModels, listMyOrgs, type Org } from "./habitat";
import { createOAuthClient, OAUTH_SCOPE } from "./oauth";
import { useAppSession } from "./session";

// Every export below is a createServerFn wrapper, safe to import from route
// components: the client bundle only gets RPC stubs (see chalk's
// functions.ts for the longer version of this note).

// completeOAuth finishes the OAuth flow that just redirected back to
// /oauth/callback: it sets the session cookie.
export const completeOAuth = createServerFn({ method: "POST" })
  .validator((input: { params: Record<string, string> }) => input)
  .handler(async ({ data }): Promise<{ to: "/" }> => {
    const { session: oauthSession } = await createOAuthClient(env).callback(
      new URLSearchParams(data.params),
    );
    const session = await useAppSession();
    await session.update({ did: oauthSession.did });
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
    const url = await createOAuthClient(env).authorize(data.handle, {
      scope: OAUTH_SCOPE,
    });
    return { redirectUrl: url.toString() };
  });

export const signOut = createServerFn({ method: "POST" }).handler(async () => {
  const session = await useAppSession();
  const did = session.data.did;
  await session.clear();
  if (did) {
    // Best-effort: forget the member's tokens too.
    try {
      await createOAuthClient(env).revoke(did);
    } catch (err) {
      console.error("[brain] revoking member session", err);
    }
  }
});

// listOrgs returns the orgs the member belongs to, for the org selector.
export const listOrgs = createServerFn({ method: "GET" }).handler(
  async (): Promise<Org[]> => {
    const { did } = await requireMember();
    return listMyOrgs(await memberSession(env, did));
  },
);

// listModels returns the ids of the LLM models an org offers the member, for
// the model picker.
export const listModels = createServerFn({ method: "GET" })
  .validator((input: { org: string }) => input)
  .handler(async ({ data }): Promise<string[]> => {
    const { did } = await requireMember();
    const agent = await memberSession(env, did);
    const org = (await listMyOrgs(agent)).find((o) => o.did === data.org);
    return org ? listLlmModels(agent, org.did) : [];
  });

// listChats returns the member's chats in an org, newest first.
export const listChats = createServerFn({ method: "GET" })
  .validator((input: { org: string }) => input)
  .handler(async ({ data }): Promise<ChatSummary[]> => {
    const { did } = await requireMember();
    return chatsForOwner(getDb(env), did, data.org);
  });

export interface ChatMessage {
  id: string;
  role: "user" | "assistant";
  content: string;
}

// getChatMessages returns a chat's messages, oldest first. A chat that
// doesn't exist yet (a new one, before its first message) has none.
export const getChatMessages = createServerFn({ method: "GET" })
  .validator((input: { chat: string }) => input)
  .handler(async ({ data }): Promise<ChatMessage[]> => {
    const { did } = await requireMember();
    const db = getDb(env);
    const owner = await chatOwner(db, data.chat);
    if (owner === undefined) return [];
    if (owner !== did) throw new Error("Chat not found");
    const rows = await messagesForChat(db, data.chat);
    return rows.flatMap((r) =>
      r.role === "user" || r.role === "assistant"
        ? [{ id: r.id, role: r.role, content: r.content }]
        : [],
    );
  });

// listOrgMcpServers returns the org's MCP servers, each with whether the
// member has connected their own credential to it.
export const listOrgMcpServers = createServerFn({ method: "GET" })
  .validator((input: { org: string }) => input)
  .handler(async ({ data }): Promise<McpServerWithStatus[]> => {
    const { did } = await requireMember();
    return listMcpServers(await memberSession(env, did), data.org as DidString);
  });

// startMcpConnect begins connecting the member to one of the org's MCP
// servers. For an oauth server it returns the authorization URL to open; for
// a token server, passing the member's token connects them right away.
export const startMcpConnect = createServerFn({ method: "POST" })
  .validator((input: { org: string; id: string; token?: string }) => input)
  .handler(async ({ data }): Promise<string | undefined> => {
    const { did } = await requireMember();
    return startMcpAuthorization(
      await memberSession(env, did),
      data.org as DidString,
      data.id,
      data.token,
    );
  });
