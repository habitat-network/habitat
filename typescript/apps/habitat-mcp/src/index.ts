#!/usr/bin/env node
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { searchHabitat } from "./search.js";

const searchUrl = process.env["HABITAT_SEARCH_URL"];
if (!searchUrl) {
  console.error("Error: HABITAT_SEARCH_URL environment variable is required.");
  console.error("Example: HABITAT_SEARCH_URL=http://localhost:8091");
  process.exit(1);
}

const server = new McpServer({
  name: "habitat-mcp",
  version: "0.1.0",
});

server.tool(
  "habitat_search",
  "Search records in Habitat spaces using full-text search",
  {
    q: z.string().describe("The search query text"),
    limit: z
      .number()
      .min(1)
      .max(100)
      .default(25)
      .optional()
      .describe("Max results to return (1-100, default 25)"),
    cursor: z
      .string()
      .optional()
      .describe("Pagination cursor returned by a previous search call"),
  },
  async ({ q, limit, cursor }) => {
    try {
      const text = await searchHabitat(searchUrl, { q, limit, cursor });
      return { content: [{ type: "text", text }] };
    } catch (err) {
      console.error("debug: full error", err)
      const message = err instanceof Error ? err.message : String(err);
      return { content: [{ type: "text", text: `Search failed: ${message}` }] };
    }
  }
);

const transport = new StdioServerTransport();
await server.connect(transport);
