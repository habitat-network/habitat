import { type Agent, xrpc } from "@atproto/lex";
import { type DidString, SpaceRef } from "@atproto/syntax";
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

// firstLlmModel is the id of the first model the org's LLM gateway offers the
// member, or null if the org has no connections.
export async function firstLlmModel(
  agent: Agent,
  org: DidString,
): Promise<string | null> {
  const res = await agent.fetchHandler(`/llm/${org}/v1/models`, {
    headers: { accept: "application/json" },
  });
  if (!res.ok) {
    throw new Error(`listing ${org}'s models failed (${res.status})`);
  }
  const body = (await res.json()) as { data?: { id?: unknown }[] };
  const id = body.data?.[0]?.id;
  return typeof id === "string" ? id : null;
}
