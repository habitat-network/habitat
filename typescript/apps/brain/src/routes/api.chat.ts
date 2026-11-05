import { createFileRoute } from "@tanstack/react-router";
import {
  chat,
  chatParamsFromRequest,
  toServerSentEventsResponse,
} from "@tanstack/ai";
import { createAnthropicChat } from "@tanstack/ai-anthropic";
import type { ModelMessage, UIMessage } from "@tanstack/ai";
import { env, waitUntil } from "cloudflare:workers";
import type { DidString } from "@atproto/syntax";
import { ensureChat, getDb, storeMessage } from "@/db";
import {
  memberSession,
  MemberUnauthorizedError,
} from "@/server/functions.server";
import { fetchOrgName, listMyOrgIds } from "@/server/habitat";
import { searchTool } from "@/server/search";
import { useAppSession } from "@/server/session";

const MODEL = "claude-sonnet-5-5";

const TITLE_MAX_CHARS = 80;

// textOf is a message's text: a UI message's text parts, or a model
// message's content.
function textOf(message: UIMessage | ModelMessage): string {
  if ("parts" in message) {
    return message.parts
      .map((p) => (p.type === "text" ? p.content : ""))
      .join("");
  }
  if (typeof message.content === "string") return message.content;
  return (message.content ?? [])
    .map((p) => (p.type === "text" ? p.content : ""))
    .join("");
}

// messageId makes the id a message is stored under from the id the chat UI
// gave it, so resending the same message overwrites it instead of repeating it.
function messageId(id: string | undefined): string {
  return id && /^[A-Za-z0-9_-]{1,64}$/.test(id) ? id : crypto.randomUUID();
}

// POST /api/chat: the chat UI's useChat connection. threadId is the chat's
// id. The route stores the user's message in the database (creating the chat,
// named by that first message), streams the agent's reply, and stores the
// reply too once it's complete. The UI reads both back through
// src/server/functions.ts's getChatMessages.
export const Route = createFileRoute("/api/chat")({
  server: {
    handlers: {
      POST: async ({ request }) => {
        const session = await useAppSession();
        const did = session.data.did as DidString | undefined;
        if (!did) {
          return Response.json({ message: "Sign in first" }, { status: 401 });
        }
        const params = await chatParamsFromRequest(request);
        // The thread id comes from the browser.
        const chatId = params.threadId;
        if (!/^[A-Za-z0-9_-]{1,64}$/.test(chatId)) {
          return Response.json({ message: "Chat not found" }, { status: 404 });
        }
        const latest = params.messages[params.messages.length - 1];
        if (!latest || latest.role !== "user") {
          return Response.json(
            { message: "Send a message first" },
            { status: 400 },
          );
        }

        try {
          const agent = await memberSession(env, did);
          const orgDids = await listMyOrgIds(agent);
          const orgs = await Promise.all(
            orgDids.map(async (orgDid) => ({
              did: orgDid,
              name: (await fetchOrgName(agent, orgDid)) ?? orgDid,
            })),
          );

          const text = textOf(latest);
          const id = "id" in latest ? (latest.id as string) : undefined;
          const db = getDb(env);
          // The first message names the chat.
          const owned = await ensureChat(
            db,
            chatId,
            did,
            text.slice(0, TITLE_MAX_CHARS) || "New chat",
          );
          if (!owned) {
            return Response.json(
              { message: "Chat not found" },
              { status: 404 },
            );
          }
          await storeMessage(db, chatId, {
            id: messageId(id),
            role: "user",
            content: text,
          });

          const abortController = new AbortController();
          const stream = chat({
            adapter: createAnthropicChat(MODEL, env.ANTHROPIC_API_KEY),
            messages: params.messages,
            threadId: params.threadId,
            systemPrompts: [
              `You are Brain, an assistant for questions about the user's organizations' data. Search before answering questions about what the organization knows, and cite what you find. The user's organizations: ${orgs.map((o) => `${o.name} (${o.did})`).join(", ") || "none"}.`,
            ],
            tools: [searchTool(agent, orgs)],
            abortController,
            middleware: [
              {
                onFinish: (_ctx, info) => {
                  if (!info.content) return;
                  waitUntil(
                    storeMessage(db, chatId, {
                      id: crypto.randomUUID(),
                      role: "assistant",
                      content: info.content,
                    }).catch((err) =>
                      console.error("[brain] storing reply", err),
                    ),
                  );
                },
              },
            ],
          });
          return toServerSentEventsResponse(stream, { abortController });
        } catch (err) {
          console.error("[brain] chat", err);
          if (err instanceof MemberUnauthorizedError) {
            await session.clear();
            return Response.json({ message: "Sign in again" }, { status: 401 });
          }
          return Response.json(
            { message: err instanceof Error ? err.message : "Chat failed" },
            { status: 502 },
          );
        }
      },
    },
  },
});
