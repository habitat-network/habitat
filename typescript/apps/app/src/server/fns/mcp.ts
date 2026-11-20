import { createServerFn } from "@tanstack/react-start";
import { network } from "api";
import { Effect, Schema } from "effect";
import { currentMember, requireOrg } from "@/server/effect/member";
import { runEffect } from "@/server/effect/run";
import { Habitat } from "@/server/effect/services";
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
        return yield* habitat.xrpc(
          did,
          network.habitat.mcp.startAuthorization.main,
          { body: { org, id: data.id, token: data.token } },
          viaGroup(org),
        );
      }),
    ),
  );

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
