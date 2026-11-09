import { toolDefinition } from "@tanstack/ai";
import type { DidString } from "@atproto/syntax";
import type { Agent } from "@atproto/lex";
import { z } from "zod";
import { searchOrgRecords } from "./habitat";

// MAX_VALUE_CHARS caps how much of each hit's record the model gets, so a few
// large records can't fill the context.
const MAX_VALUE_CHARS = 4000;

// searchTool is the agent's built-in tool: full-text search over the records in
// the spaces one of the member's orgs owns that the member can read
// (network.habitat.space.searchRecords). It runs as the member, so it never
// returns anything they couldn't read themselves.
export function searchTool(agent: Agent, org: DidString) {
  return toolDefinition({
    name: "search",
    description:
      "Full-text search over the records the user can read in the current organization, most relevant first. Use it to look things up before answering questions about their organization's data.",
    inputSchema: z.object({
      query: z.string().describe("The text to search for"),
    }),
  }).server(async ({ query }) => {
    const hits = await searchOrgRecords(agent, org, query, 10);
    return {
      results: hits.map((h) => ({
        uri: h.uri,
        snippet: h.snippet,
        record: JSON.stringify(h.value).slice(0, MAX_VALUE_CHARS),
      })),
    };
  });
}
