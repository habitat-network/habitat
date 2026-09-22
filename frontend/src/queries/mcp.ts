import type { AuthManager } from "internal";
import { xrpc, type DidString, type UriString } from "@atproto/lex";
import { queryOptions } from "@tanstack/react-query";
import { network } from "api";
import { pearAgent } from "./pearAgent";

export type McpServer = network.habitat.mcp.defs.Server;
export type McpServerWithStatus = network.habitat.mcp.listServers.ServerWithStatus;

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

// addMcpServer configures a new MCP server for org. Requires the caller to
// hold the community.configure action.
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

// updateMcpServer updates an org's configured MCP server. Requires the
// caller to hold the community.configure action.
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
// community.configure action.
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

// startMcpAuthorization begins an OAuth authorization-code flow for the
// caller to connect to an org-configured MCP server, returning the URL to
// redirect the browser to. redirectUri is where the browser lands once
// authorization completes.
export async function startMcpAuthorization(
  authManager: AuthManager,
  org: DidString,
  id: string,
  redirectUri: string,
) {
  const response = await xrpc(
    pearAgent(authManager, `${org}#habitat`),
    network.habitat.mcp.startAuthorization.main,
    { body: { org, id, redirectUri: redirectUri as UriString } },
  );
  return response.body.authorizationUrl;
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
