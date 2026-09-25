import type { AuthManager } from "internal";
import { xrpc, type DidString } from "@atproto/lex";
import { queryOptions } from "@tanstack/react-query";
import { network } from "api";
import { pearAgent } from "./pearAgent";

export type McpServer = network.habitat.mcp.defs.Server;
export type McpServerWithStatus =
  network.habitat.mcp.listServers.ServerWithStatus;

// orgMcpServersQueryOptions lists the MCP servers configured for org, along
// with whether the caller has connected their own credential to each one.
// Requires service-auth, so it's proxied to the org's own habitat instance
// via pearAgent rather than the caller's OAuth session.
export function orgMcpServersQueryOptions(
  org: DidString,
  authManager: AuthManager,
) {
  return queryOptions({
    queryKey: ["mcp", "servers", org],
    queryFn: async (): Promise<McpServerWithStatus[]> => {
      const response = await xrpc(
        pearAgent(authManager, `${org}#habitat`),
        network.habitat.mcp.listServers.main,
        { params: { org } },
      );
      return response.body.servers;
    },
  });
}

// addMcpServer adds an MCP server to org by writing its record: name,
// description, url and authType. Nobody signs in yet; for an oauth server,
// a member connects afterward with startMcpAuthorization, while a manual
// server needs no auth and every member is connected automatically.
// Requires the caller to hold the mcp.configure action.
export async function addMcpServer(
  authManager: AuthManager,
  org: DidString,
  input: Omit<network.habitat.mcp.addServer.$InputBody, "org">,
) {
  const response = await xrpc(
    pearAgent(authManager, `${org}#habitat`),
    network.habitat.mcp.addServer.main,
    { body: { org, ...input } },
  );
  return response.body.server;
}

// updateMcpServer updates an org's configured MCP server's display name or
// description. Requires the caller to hold the mcp.configure action.
export async function updateMcpServer(
  authManager: AuthManager,
  org: DidString,
  input: Omit<network.habitat.mcp.updateServer.$InputBody, "org">,
) {
  const response = await xrpc(
    pearAgent(authManager, `${org}#habitat`),
    network.habitat.mcp.updateServer.main,
    { body: { org, ...input } },
  );
  return response.body.server;
}

// removeMcpServer deletes an org's configured MCP server, along with any
// stored user credentials for it. Requires the caller to hold the
// mcp.configure action.
export async function removeMcpServer(
  authManager: AuthManager,
  org: DidString,
  id: string,
) {
  await xrpc(
    pearAgent(authManager, `${org}#habitat`),
    network.habitat.mcp.removeServer.main,
    { body: { org, id } },
  );
}

// startMcpAuthorization begins a Nango Connect session for the caller to
// authorize against an existing org-configured MCP server, returning a
// session token for the Nango frontend SDK's Connect UI.
export async function startMcpAuthorization(
  authManager: AuthManager,
  org: DidString,
  id: string,
) {
  const response = await xrpc(
    pearAgent(authManager, `${org}#habitat`),
    network.habitat.mcp.startAuthorization.main,
    { body: { org, id } },
  );
  return response.body.sessionToken;
}

// disconnectMcpServer removes the caller's stored credential for an
// org-configured MCP server.
export async function disconnectMcpServer(
  authManager: AuthManager,
  org: DidString,
  id: string,
) {
  await xrpc(
    pearAgent(authManager, `${org}#habitat`),
    network.habitat.mcp.disconnectServer.main,
    { body: { org, id } },
  );
}
