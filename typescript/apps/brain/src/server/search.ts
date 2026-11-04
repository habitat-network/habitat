import { toolDefinition } from "@tanstack/ai";
import { ensureValidDid, type DidString } from "@atproto/syntax";
import type { Agent } from "@atproto/lex";
import { z } from "zod";
import { searchOrgRecords } from "./habitat";

// MAX_VALUE_CHARS caps how much of each hit's record the model gets, so a few
// large records can't fill the context.
const MAX_VALUE_CHARS = 4000;

export interface SearchableOrg {
  did: DidString;
  name: string;
}

// searchTool is the agent's only tool: full-text search over the records in
// the spaces one of the member's orgs owns that the member can read
// (network.habitat.space.searchRecords). It runs as the member, so it never
// returns anything they couldn't read themselves.
export function searchTool(agent: Agent, orgs: readonly SearchableOrg[]) {
  return toolDefinition({
    name: "search",
    description:
      "Full-text search over the records the user can read in one of their organizations, most relevant first. Use it to look things up before answering questions about their organization's data.",
    inputSchema: z.object({
      query: z.string().describe("The text to search for"),
      org: z
        .string()
        .optional()
        .describe(
          `DID of the organization to search. Optional when the user belongs to one. Their organizations: ${orgs.map((o) => `${o.name} (${o.did})`).join(", ")}`,
        ),
    }),
  }).server(async ({ query, org }) => {
    const target = org ?? (orgs.length === 1 ? orgs[0].did : undefined);
    if (!target) {
      return { error: "Say which organization to search with `org`." };
    }
    ensureValidDid(target);
    if (!orgs.some((o) => o.did === target)) {
      return { error: "The user isn't a member of that organization." };
    }
    const hits = await searchOrgRecords(agent, target as DidString, query, 10);
    return {
      results: hits.map((h) => ({
        uri: h.uri,
        snippet: h.snippet,
        record: JSON.stringify(h.value).slice(0, MAX_VALUE_CHARS),
      })),
    };
  });
}
