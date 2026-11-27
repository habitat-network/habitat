import { createServerFn } from "@tanstack/react-start";
import { network } from "api";
import { Effect, Schema } from "effect";
import { BadRequest } from "@/server/effect/errors";
import { currentMember, requireOrg } from "@/server/effect/member";
import { runEffect } from "@/server/effect/run";
import { AppEnv, Habitat } from "@/server/effect/services";
import { viaGroup } from "@/server/groups";

// MCP servers: the tools members' agents can call inside the group, served
// through pear's MCP gateway. Adding, editing and removing a server needs
// mcp.configure; connecting one's own account (OAuth or a token) is each
// member's own.

export type McpServerView = network.habitat.mcp.listServers.ServerWithStatus;
type Uri = `${string}:${string}`;

export type SearchBackend = network.habitat.mcp.search.Backend;

// SearchResultView is one search hit, flattened to what the page shows (a
// record's value stays on the server).
export interface SearchResultView {
  backend: string;
  kind: string;
  id: string;
  title: string | null;
  url: string | null;
  snippet: string | null;
  space: string | null;
  collection: string | null;
}

export interface SearchPage {
  results: SearchResultView[];
  cursor: string | null;
  backends: SearchBackend[];
}

const OrgInput = Schema.Struct({ org: Schema.String });
const ServerInput = Schema.Struct({ org: Schema.String, id: Schema.String });

export const listMcpServers = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(OrgInput))
  .handler(({ data }) =>
    runEffect(
      "mcp.list",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        const { servers } = yield* habitat.xrpc(
          did,
          network.habitat.mcp.listServers.main,
          { params: { org } },
          viaGroup(org),
        );
        return servers;
      }),
    ),
  );

// getOrgMcpUrl is the org's MCP endpoint on the Habitat instance (pear's
// mcpserver.Path + "/" + org), which MCP clients are pointed at.
export const getOrgMcpUrl = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(OrgInput))
  .handler(({ data }) =>
    runEffect(
      "mcp.url",
      Effect.gen(function* () {
        const { did } = yield* requireOrg(data.org);
        const env = yield* AppEnv;
        return new URL(`/mcp/${did}`, env.APP_HABITAT_URL).toString();
      }),
    ),
  );

const AUTH_TYPES = ["oauth", "token", "manual"] as const;
const PRESETS = [
  "google-drive",
  "google-cloud",
  "google-calendar",
  "gmail",
  "slack",
  "github",
] as const;

export const addMcpServer = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({
        org: Schema.String,
        name: Schema.String,
        description: Schema.optional(Schema.String),
        preset: Schema.optional(Schema.Literals(PRESETS)),
        url: Schema.optional(Schema.String),
        authType: Schema.optional(Schema.Literals(AUTH_TYPES)),
        clientId: Schema.optional(Schema.String),
        clientSecret: Schema.optional(Schema.String),
      }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "mcp.add",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        const { org: _org, url, ...input } = data;
        const { server } = yield* habitat.xrpc(
          did,
          network.habitat.mcp.addServer.main,
          { body: { org, ...input, url: url as Uri | undefined } },
          viaGroup(org),
        );
        yield* Effect.logInfo("added MCP server", { org, id: server.id });
        return server;
      }),
    ),
  );

export const updateMcpServer = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({
        org: Schema.String,
        id: Schema.String,
        description: Schema.optional(Schema.String),
        url: Schema.optional(Schema.String),
      }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "mcp.update",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        const { server } = yield* habitat.xrpc(
          did,
          network.habitat.mcp.updateServer.main,
          {
            body: {
              org,
              id: data.id,
              description: data.description,
              url: data.url as Uri | undefined,
            },
          },
          viaGroup(org),
        );
        return server;
      }),
    ),
  );

export const removeMcpServer = createServerFn({ method: "POST" })
  .validator(Schema.decodeUnknownSync(ServerInput))
  .handler(({ data }) =>
    runEffect(
      "mcp.remove",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        yield* habitat.xrpc(
          did,
          network.habitat.mcp.removeServer.main,
          { body: { org, id: data.id } },
          viaGroup(org),
        );
        yield* Effect.logInfo("removed MCP server", { org, id: data.id });
      }),
    ),
  );

// connectMcpServer starts connecting the member's own account to a server:
// for an OAuth server it returns the provider's authorization URL to send
// the browser to; for a token server it stores the token given.
export const connectMcpServer = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({
        org: Schema.String,
        id: Schema.String,
        token: Schema.optional(Schema.String),
      }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "mcp.connect",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        const out = yield* habitat.xrpc(
          did,
          network.habitat.mcp.startAuthorization.main,
          { body: { org, id: data.id, token: data.token } },
          viaGroup(org),
        );
        if (data.token) {
          // pear stores a token without trying it, so try it here: a token
          // the server doesn't accept, or a server that can't be reached,
          // shouldn't leave the member shown as connected.
          const probe = yield* habitat.xrpc(
            did,
            network.habitat.mcp.debug.main,
            { body: { org, id: data.id, method: "tools/list" } },
            viaGroup(org),
          );
          if (!probe.ok) {
            yield* habitat
              .xrpc(
                did,
                network.habitat.mcp.disconnectServer.main,
                { body: { org, id: data.id } },
                viaGroup(org),
              )
              .pipe(Effect.ignore);
            return yield* new BadRequest({
              message: connectFailureMessage(probe.error),
            });
          }
        }
        return out;
      }),
    ),
  );

// connectFailureMessage is what a member is told when a server turned down
// the token they gave.
export function connectFailureMessage(error: string | undefined): string {
  return `Couldn't reach this server with that token${error ? `: ${error}` : ""}. Check the token and the server's URL.`;
}

export const disconnectMcpServer = createServerFn({ method: "POST" })
  .validator(Schema.decodeUnknownSync(ServerInput))
  .handler(({ data }) =>
    runEffect(
      "mcp.disconnect",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        yield* habitat.xrpc(
          did,
          network.habitat.mcp.disconnectServer.main,
          { body: { org, id: data.id } },
          viaGroup(org),
        );
      }),
    ),
  );

export interface McpToolView {
  name: string;
  title: string | null;
  description: string | null;
}

export interface McpToolsView {
  tools: McpToolView[];
  // Why the tools couldn't be listed, e.g. the caller isn't connected yet.
  error: string | null;
}

// listMcpTools lists the tools a server offers, by making a tools/list
// request to it as the member (so it needs the member to be connected).
export const listMcpTools = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(ServerInput))
  .handler(({ data }) =>
    runEffect(
      "mcp.tools",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        const out = yield* habitat.xrpc(
          did,
          network.habitat.mcp.debug.main,
          { body: { org, id: data.id, method: "tools/list" } },
          viaGroup(org),
        );
        if (!out.ok || !out.result) {
          return {
            tools: [],
            error: out.error ?? "The server returned no result",
          } satisfies McpToolsView;
        }
        const parsed = JSON.parse(out.result) as {
          tools?: {
            name: string;
            title?: string;
            description?: string;
          }[];
        };
        return {
          tools: (parsed.tools ?? []).map((t) => ({
            name: t.name,
            title: t.title ?? null,
            description: t.description ?? null,
          })),
          error: null,
        } satisfies McpToolsView;
      }),
    ),
  );

// searchGroup runs one page of a federated search over the group's Habitat
// records and the member's connected MCP servers: a new search with query,
// or the next page of one with cursor. pear waits up to 10s for results.
export const searchGroup = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({
        org: Schema.String,
        query: Schema.optional(Schema.String),
        cursor: Schema.optional(Schema.String),
      }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "search.query",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        const page = yield* habitat.xrpc(
          did,
          network.habitat.mcp.search.main,
          {
            validateResponse: false,
            body: data.cursor
              ? { org, cursor: data.cursor }
              : { org, query: data.query ?? "" },
          },
          viaGroup(org),
        );
        yield* Effect.logDebug("search", {
          results: page.results.length,
          backends: page.backends.map((b) => `${b.id}:${b.status}`),
        });
        return {
          results: page.results.map((r): SearchResultView => ({
            backend: r.backend,
            kind: r.kind,
            id: r.id,
            title: r.title ?? null,
            url: r.url ?? null,
            snippet: r.snippet ?? null,
            space: r.record?.space ?? null,
            collection: r.record?.collection ?? null,
          })),
          cursor: page.cursor ?? null,
          backends: page.backends,
        } satisfies SearchPage;
      }),
    ),
  );

// listConnectedApps lists the apps the member has authorized to act for
// them.
export const listConnectedApps = createServerFn({ method: "GET" }).handler(() =>
  runEffect(
    "connections.apps",
    Effect.gen(function* () {
      const did = yield* currentMember;
      const habitat = yield* Habitat;
      const { apps } = yield* habitat.xrpc(
        did,
        network.habitat.listConnectedApps.main,
        {},
      );
      return apps;
    }),
  ),
);
