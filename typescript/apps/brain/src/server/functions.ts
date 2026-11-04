import { createServerFn } from "@tanstack/react-start";
import { env } from "cloudflare:workers";
import {
  chatsForOwner,
  getDb,
  messagesForSpace,
  type ChatSummary,
} from "../db";
import {
  memberSession,
  ownedChatSpace,
  requireMember,
  resolveHandle,
} from "./functions.server";
import { createChatSpace, listMyChatSpaces } from "./habitat";
import { createOAuthClient, OAUTH_SCOPE } from "./oauth";
import { useAppSession } from "./session";
import { watchSpaces } from "./sync";

// Every export below is a createServerFn wrapper, safe to import from route
// components: the client bundle only gets RPC stubs (see chalk's
// functions.ts for the longer version of this note).

// completeOAuth finishes the OAuth flow that just redirected back to
// /oauth/callback: it sets the session cookie and starts syncing the chats
// the member already has (e.g. created on another deployment).
export const completeOAuth = createServerFn({ method: "POST" })
  .validator((input: { params: Record<string, string> }) => input)
  .handler(async ({ data }): Promise<{ to: "/" }> => {
    const { session: oauthSession } = await createOAuthClient(env).callback(
      new URLSearchParams(data.params),
    );
    const session = await useAppSession();
    await session.update({ did: oauthSession.did });
    try {
      await watchSpaces(
        env,
        await listMyChatSpaces(oauthSession, oauthSession.did),
      );
    } catch (err) {
      // Not worth failing the sign-in: the next sign-in tries again.
      console.error("[brain] watching existing chats", err);
    }
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

// listChats returns the member's synced chats, newest first.
export const listChats = createServerFn({ method: "GET" }).handler(
  async (): Promise<ChatSummary[]> => {
    const { did } = await requireMember();
    return chatsForOwner(getDb(env), did);
  },
);

// createChat makes a new, empty chat: its own space, owned by the member. The
// chat shows up in listChats once its first message names it.
export const createChat = createServerFn({ method: "POST" }).handler(
  async (): Promise<{ space: string }> => {
    const { did } = await requireMember();
    const space = await createChatSpace(await memberSession(env, did), did);
    await watchSpaces(env, [space]);
    return { space };
  },
);

export interface ChatMessage {
  id: string;
  role: "user" | "assistant";
  content: string;
}

// getChatMessages returns a chat's synced messages, oldest first.
export const getChatMessages = createServerFn({ method: "GET" })
  .validator((input: { space: string }) => input)
  .handler(async ({ data }): Promise<ChatMessage[]> => {
    const { did } = await requireMember();
    const space = ownedChatSpace(data.space, did);
    const rows = await messagesForSpace(getDb(env), space);
    return rows.flatMap((r) =>
      r.role === "user" || r.role === "assistant"
        ? [{ id: r.id, role: r.role, content: r.content }]
        : [],
    );
  });
