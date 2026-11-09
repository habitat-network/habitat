import { type Agent, xrpc } from "@atproto/lex";
import { type DidString, type NsidString, SpaceRef } from "@atproto/syntax";
import { network } from "api";

// Calls Brain makes against the Habitat instance, each as the signed-in
// member (an OAuthSession is an @atproto/lex Agent).

// listMyOrgIds lists every opensocial org the agent's subject belongs to:
// every community.opensocial.members space it holds a membership in (same
// query as chalk's and drop's listMyOrgIds).
export async function listMyOrgIds(agent: Agent): Promise<DidString[]> {
  const { body } = await xrpc(agent, network.habitat.space.listSpaces.main, {
    params: { spaceType: "community.opensocial.members" },
  });
  return body.spaces.map((s) => SpaceRef.parse(s.uri).spaceDid);
}

// fetchOrgName reads an org's display name off its community.opensocial
// profile record. Any member can read the org's about space, so the
// member's own agent works. Null when the read fails, so callers can fall
// back to the DID.
export async function fetchOrgName(
  agent: Agent,
  orgDid: DidString,
): Promise<string | null> {
  try {
    const { body } = await xrpc(agent, network.habitat.space.getRecord.main, {
      params: {
        space: new SpaceRef(
          orgDid,
          "community.opensocial.about",
          "self",
        ).toString(),
        repo: orgDid,
        collection: "community.opensocial.profile",
        rkey: "self",
      },
    });
    const name = (body.value as { name?: unknown }).name;
    return typeof name === "string" ? name : null;
  } catch {
    return null;
  }
}

export interface SearchHit {
  uri: string;
  space: string;
  snippet?: string;
  value: unknown;
}

// searchOrgRecords full-text searches the spaces an org owns that the member
// can read (network.habitat.space.searchRecords), most relevant first.
export async function searchOrgRecords(
  agent: Agent,
  org: DidString,
  q: string,
  limit: number,
): Promise<SearchHit[]> {
  const { body } = await xrpc(agent, network.habitat.space.searchRecords.main, {
    params: { q, org, limit },
  });
  return body.records.map((r) => ({
    uri: r.uri,
    space: r.space,
    snippet: r.snippet,
    value: r.value,
  }));
}

// llmBaseUrl is where the org's LLM gateway serves its OpenAI-compatible API
// (pear's /llm/{org}/v1); the member's own session is the API key.
export function llmBaseUrl(habitatUrl: string, org: DidString): string {
  return `${habitatUrl.replace(/\/+$/, "")}/llm/${org}/v1`;
}

// listLlmModels is the ids of the models the org's LLM gateway offers the
// member.
export async function listLlmModels(
  agent: Agent,
  org: DidString,
): Promise<string[]> {
  const res = await agent.fetchHandler(`/llm/${org}/v1/models`, {
    headers: { accept: "application/json" },
  });
  if (!res.ok) {
    throw new Error(`listing ${org}'s models failed (${res.status})`);
  }
  const body = (await res.json()) as { data?: { id?: unknown }[] };
  return (body.data ?? []).flatMap((m) =>
    typeof m.id === "string" ? [m.id] : [],
  );
}

// firstLlmModel is the id of the first model the org's LLM gateway offers the
// member, or null if the org has no connections.
export async function firstLlmModel(
  agent: Agent,
  org: DidString,
): Promise<string | null> {
  return (await listLlmModels(agent, org))[0] ?? null;
}

// mcpUrl is where pear's MCP gateway serves the org's merged tools (the
// streamable-HTTP endpoint at /mcp/{org}).
export function mcpUrl(habitatUrl: string, org: DidString): string {
  return `${habitatUrl.replace(/\/+$/, "")}/mcp/${org}`;
}

export interface Org {
  did: DidString;
  name: string;
}

// listMyOrgs lists the orgs the member belongs to, with their names.
export async function listMyOrgs(agent: Agent): Promise<Org[]> {
  const dids = await listMyOrgIds(agent);
  return Promise.all(
    dids.map(async (did) => ({
      did,
      name: (await fetchOrgName(agent, did)) ?? did,
    })),
  );
}

export type LlmConnection = network.habitat.llm.defs.Connection;
export type McpServer = network.habitat.mcp.defs.Server;

// What the member may configure in an org, from the org's
// community.opensocial permissions record and their own membership roles
// (mirrors Store.CheckAction in Go: a community without a permissions record
// falls back to authorizing its admins).
export interface ConfigAccess {
  mcp: boolean;
  models: boolean;
}

// getConfigAccess reads the member's roles and the org's action bindings from
// its members space. The server still enforces them; this only decides
// whether to show the controls.
export async function getConfigAccess(
  agent: Agent,
  org: DidString,
  member: DidString,
): Promise<ConfigAccess> {
  const space = new SpaceRef(
    org,
    "community.opensocial.members",
    "self",
  ).toString();
  const read = async (collection: NsidString, rkey: string) => {
    try {
      const { body } = await xrpc(agent, network.habitat.space.getRecord.main, {
        params: { space, repo: org, collection, rkey },
      });
      return body.value as Record<string, unknown>;
    } catch {
      return undefined;
    }
  };
  const [membership, permissions] = await Promise.all([
    read("community.opensocial.membership" as NsidString, member),
    read("community.opensocial.permissions" as NsidString, "self"),
  ]);
  const roles = (membership?.roles as string[] | undefined) ?? [];
  const bindings =
    (permissions?.bindings as { action: string; roles: string[] }[]) ?? [];
  const allowed = (action: string) =>
    bindings.length === 0
      ? roles.includes("admin")
      : (bindings.find((b) => b.action === action)?.roles ?? []).some((r) =>
          roles.includes(r),
        );
  return {
    mcp: allowed("mcp.configure"),
    // Model connections have no action of their own; they are part of
    // configuring the community.
    models: allowed("community.configure"),
  };
}

export async function listLlmConnections(
  agent: Agent,
  org: DidString,
): Promise<LlmConnection[]> {
  const { body } = await xrpc(agent, network.habitat.llm.listConnections.main, {
    params: { org },
  });
  return body.connections;
}
