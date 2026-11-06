import { type Agent, xrpc } from "@atproto/lex";
import type { DidString } from "@atproto/lex";
import { network } from "api";

export type McpServerWithStatus =
  network.habitat.mcp.listServers.ServerWithStatus;

// listMcpServers lists the MCP servers configured for org, along with whether
// the caller has connected their own credential to each one.
export async function listMcpServers(
  agent: Agent,
  org: DidString,
): Promise<McpServerWithStatus[]> {
  const response = await xrpc(agent, network.habitat.mcp.listServers.main, {
    params: { org },
  });
  return response.body.servers;
}

// startMcpAuthorization begins connecting the caller to an org-configured MCP
// server. For an oauth server it resolves to the server's authorization URL,
// for the caller to open; pear stores their tokens once they approve. For a
// token server, token is the caller's own access token: pear stores it, which
// connects the caller right away, so it resolves to undefined.
export async function startMcpAuthorization(
  agent: Agent,
  org: DidString,
  id: string,
  token?: string,
): Promise<string | undefined> {
  const response = await xrpc(
    agent,
    network.habitat.mcp.startAuthorization.main,
    { body: { org, id, token } },
  );
  return response.body.authorizationUrl;
}
