import { createMCPClient } from "@tanstack/ai-mcp";
import { toolDefinition } from "@tanstack/ai";
import type { JSONSchema } from "@tanstack/ai";
import type { Agent } from "@atproto/lex";
import { CfWorkerJsonSchemaValidator } from "@modelcontextprotocol/client/validators/cf-worker";
import type { DidString } from "@atproto/syntax";
import { mcpUrl } from "./habitat";

// Brain's tools beyond search come from pear's MCP gateway: the org's MCP
// endpoint (pear's /mcp/{org}) merges the tools of every MCP server the
// member has connected and selected, so one MCP client gives the model all of
// them. Following TanStack AI's MCP guide, tools are discovered with
// `createMCPClient`; because discovering them costs a handshake plus a
// tools/list on every chat request, the definitions are cached and only the
// calls themselves open a connection.

const CACHE_TTL_MS = 5 * 60 * 1000;

interface CachedTool {
  name: string;
  description: string;
  inputSchema: JSONSchema;
}

interface CacheEntry {
  at: number;
  tools: Promise<CachedTool[]>;
}

// Per-isolate cache of each member's tool definitions in each org. Tool
// availability differs per member (their own connections and selection), so
// the key includes the member. A stale entry just means the model sees an old
// list for up to CACHE_TTL_MS; calling a tool that has since gone fails with
// the server's own error.
const cache = new Map<string, CacheEntry>();

// connect opens an MCP client on the org's gateway endpoint as the member.
// MCP's transport takes a fetch, so each request goes out through the
// member's OAuth session (which adds the DPoP-bound Authorization).
function connect(agent: Agent, habitatUrl: string, org: DidString) {
  return createMCPClient({
    transport: {
      type: "http",
      url: mcpUrl(habitatUrl, org),
      fetch: (input, init) => {
        const req = input instanceof Request ? input : null;
        const url = new URL(req ? req.url : input.toString());
        return agent.fetchHandler(
          `${url.pathname}${url.search}` as `/${string}`,
          init ?? req ?? {},
        );
      },
    },
    name: "brain",
    // The SDK's default validator compiles schemas with new Function, which
    // Workers forbid.
    clientOptions: { jsonSchemaValidator: new CfWorkerJsonSchemaValidator() },
  });
}

async function discover(
  agent: Agent,
  habitatUrl: string,
  org: DidString,
): Promise<CachedTool[]> {
  const client = await connect(agent, habitatUrl, org);
  try {
    const tools = await client.tools();
    return tools.map((t) => ({
      name: t.name,
      description: t.description,
      inputSchema: t.inputSchema as JSONSchema,
    }));
  } finally {
    await client.close();
  }
}

// errorText is the readable message of a failed tool result: its text
// content, else the content as JSON.
function errorText(content: unknown): string {
  if (Array.isArray(content)) {
    const text = content
      .flatMap((c) =>
        c && typeof c === "object" && c.type === "text" ? [String(c.text)] : [],
      )
      .join("\n");
    if (text) return text;
  }
  return JSON.stringify(content ?? "tool call failed");
}

// gatewayTools returns the member's tools in the org, from cache when fresh.
// A failure to reach the gateway is thrown, so the chat route reports it to
// the client instead of the chat quietly having no connected tools.
export async function gatewayTools(
  agent: Agent,
  habitatUrl: string,
  org: DidString,
  reserved: ReadonlySet<string>,
) {
  const key = `${agent.did}|${org}`;
  let entry = cache.get(key);
  if (!entry || Date.now() - entry.at > CACHE_TTL_MS) {
    const tools = discover(agent, habitatUrl, org);
    entry = { at: Date.now(), tools };
    cache.set(key, entry);
    // Don't cache failures.
    tools.catch(() => {
      if (cache.get(key) === entry) cache.delete(key);
    });
  }
  let defs: CachedTool[];
  try {
    defs = await entry.tools;
  } catch (err) {
    console.error("[brain] listing MCP gateway tools", err);
    throw new Error(
      `Couldn't list your connected tools: ${err instanceof Error ? err.message : String(err)}`,
    );
  }
  return defs
    .filter((def) => !reserved.has(def.name))
    .map((def) =>
      toolDefinition({
        name: def.name,
        description: def.description,
        inputSchema: def.inputSchema,
      }).server(async (args) => {
        // A fresh connection per call: cached definitions outlive the
        // request that fetched them, and a connection can't.
        const client = await connect(agent, habitatUrl, org);
        try {
          const result = await client.callTool(
            def.name,
            args as Record<string, unknown>,
          );
          if (result.isError) {
            throw new Error(errorText(result.content));
          }
          return result.structuredContent ?? result.content;
        } finally {
          await client.close();
        }
      }),
    );
}
