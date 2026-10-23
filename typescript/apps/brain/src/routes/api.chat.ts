import { createFileRoute } from "@tanstack/react-router";
import {
  chat,
  chatParamsFromRequest,
  toServerSentEventsResponse,
} from "@tanstack/ai";
import { createAnthropicChat } from "@tanstack/ai-anthropic";
import type { ModelMessage, UIMessage } from "@tanstack/ai";
import { env, waitUntil } from "cloudflare:workers";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import {
  memberSession,
  MemberUnauthorizedError,
  ownedChatSpace,
} from "@/server/functions.server";
import {
  fetchOrgName,
  listMyOrgIds,
  putMessageRecord,
  putSessionRecord,
} from "@/server/habitat";
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

// recordKey makes the key a message is stored under from the id the chat UI
// gave it, so resending the same message overwrites it instead of repeating it.
function recordKey(id: string | undefined): string {
  return id && /^[A-Za-z0-9_-]{1,64}$/.test(id) ? id : crypto.randomUUID();
}

// POST /api/chat: the chat UI's useChat connection. threadId is the chat's
// space. The route stores the user's message in that space, streams the
// agent's reply, and stores the reply too once it's complete. The UI reads
// both back through the syncer (see src/server/functions.ts's
// getChatMessages), not from here.
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
        let space: SpaceRefString;
        try {
          space = ownedChatSpace(params.threadId, did);
        } catch {
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
          await putMessageRecord(agent, did, space, {
            rkey: recordKey(id),
            role: "user",
            content: text,
          });
          // The first message names the chat.
          if (params.messages.length === 1) {
            await putSessionRecord(
              agent,
              did,
              space,
              text.slice(0, TITLE_MAX_CHARS) || "New chat",
            );
          }

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
                    putMessageRecord(agent, did, space, {
                      rkey: crypto.randomUUID(),
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
