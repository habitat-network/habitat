import type { Agent } from "@atproto/lex";
import { describe, expect, it } from "vitest";
import { searchTool } from "@/server/search";

const ORG = "did:web:org.example" as const;
const OTHER = "did:web:other.example" as const;
const SPACE_URI = "at://did:plc:alice/space/network.habitat.brain/3kchat";
const RECORD_URI = `${SPACE_URI}/did:plc:alice/network.habitat.brain.message/a`;

// An agent whose fetches are recorded and answered with one search hit.
function fakeAgent() {
  const urls: string[] = [];
  const agent: Agent = {
    did: "did:plc:alice",
    fetchHandler: async (path) => {
      urls.push(path);
      return Response.json({
        records: [
          {
            uri: RECORD_URI,
            space: SPACE_URI,
            cid: "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm",
            value: { text: "hello" },
            snippet: "<mark>hello</mark>",
          },
        ],
      });
    },
  };
  return { agent, urls };
}

const run = (tool: ReturnType<typeof searchTool>, input: unknown) =>
  (tool.execute as (input: unknown) => Promise<unknown>)(input);

describe("searchTool", () => {
  it("searches the only org by default", async () => {
    const { agent, urls } = fakeAgent();
    const tool = searchTool(agent, [{ did: ORG, name: "Org" }]);
    const result = await run(tool, { query: "hello" });
    expect(urls).toHaveLength(1);
    const url = new URL(urls[0], "https://pear.example");
    expect(url.pathname).toBe("/xrpc/network.habitat.space.searchRecords");
    expect(url.searchParams.get("org")).toBe(ORG);
    expect(url.searchParams.get("q")).toBe("hello");
    expect(result).toEqual({
      results: [
        {
          uri: RECORD_URI,
          snippet: "<mark>hello</mark>",
          record: '{"text":"hello"}',
        },
      ],
    });
  });

  it("asks which org when the user has several", async () => {
    const { agent, urls } = fakeAgent();
    const tool = searchTool(agent, [
      { did: ORG, name: "Org" },
      { did: OTHER, name: "Other" },
    ]);
    expect(await run(tool, { query: "hello" })).toEqual({
      error: "Say which organization to search with `org`.",
    });
    expect(urls).toEqual([]);
  });

  it("refuses an org the user isn't in", async () => {
    const { agent, urls } = fakeAgent();
    const tool = searchTool(agent, [{ did: ORG, name: "Org" }]);
    expect(await run(tool, { query: "hello", org: OTHER })).toEqual({
      error: "The user isn't a member of that organization.",
    });
    expect(urls).toEqual([]);
  });
});
