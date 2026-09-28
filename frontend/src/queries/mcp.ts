import type { AuthManager } from "internal";
import { xrpc, type DidString } from "@atproto/lex";
import { mutationOptions, queryOptions } from "@tanstack/react-query";
import { network } from "api";
import { pearAgent } from "./pearAgent";

export type McpServer = network.habitat.mcp.defs.Server;
export type McpServerWithStatus =
  network.habitat.mcp.listServers.ServerWithStatus;

export const mcpServersQueryKey = (org: DidString) =>
  ["mcp", "servers", org] as const;

// orgMcpServersQueryOptions lists the MCP servers configured for org, along
// with whether the caller has connected their own credential to each one.
// Requires service-auth, so it's proxied to the org's own habitat instance
// via pearAgent rather than the caller's OAuth session.
export function orgMcpServersQueryOptions(
  org: DidString,
  authManager: AuthManager,
) {
  return queryOptions({
    queryKey: mcpServersQueryKey(org),
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

// The mutations below are mutationOptions factories, so components get
// loading and error state from useMutation. Each one that changes what
// listServers returns refetches it before resolving, so isPending covers the
// refresh and callers can close their dialogs from onSuccess.

// addMcpServerMutationOptions adds an MCP server to org by writing its
// record: name, description, url and authType. Nobody signs in yet; for an
// oauth server, a member connects afterward with
// startMcpAuthorizationMutationOptions, while a manual server needs no auth
// and every member is connected automatically. Requires the caller to hold
// the mcp.configure action.
export const addMcpServerMutationOptions = (
  authManager: AuthManager,
  org: DidString,
) =>
  mutationOptions({
    mutationFn: async (
      input: Omit<network.habitat.mcp.addServer.$InputBody, "org">,
      { client },
    ) => {
      const response = await xrpc(
        pearAgent(authManager, `${org}#habitat`),
        network.habitat.mcp.addServer.main,
        { body: { org, ...input } },
      );
      await client.invalidateQueries({ queryKey: mcpServersQueryKey(org) });
      return response.body.server;
    },
  });

// updateMcpServerMutationOptions updates an org's configured MCP server's
// description or, for a manual server, its URL. Requires the caller to hold
// the mcp.configure action.
export const updateMcpServerMutationOptions = (
  authManager: AuthManager,
  org: DidString,
) =>
  mutationOptions({
    mutationFn: async (
      input: Omit<network.habitat.mcp.updateServer.$InputBody, "org">,
      { client },
    ) => {
      const response = await xrpc(
        pearAgent(authManager, `${org}#habitat`),
        network.habitat.mcp.updateServer.main,
        { body: { org, ...input } },
      );
      await client.invalidateQueries({ queryKey: mcpServersQueryKey(org) });
      return response.body.server;
    },
  });

// removeMcpServerMutationOptions deletes an org's configured MCP server,
// identified by the mutation's id variable, along with any stored user
// credentials for it. Requires the caller to hold the mcp.configure action.
export const removeMcpServerMutationOptions = (
  authManager: AuthManager,
  org: DidString,
) =>
  mutationOptions({
    mutationFn: async (id: string, { client }) => {
      await xrpc(
        pearAgent(authManager, `${org}#habitat`),
        network.habitat.mcp.removeServer.main,
        { body: { org, id } },
      );
      await client.invalidateQueries({ queryKey: mcpServersQueryKey(org) });
    },
  });

// startMcpAuthorizationMutationOptions begins a Nango Connect session for
// the caller to authorize against an existing org-configured MCP server,
// identified by the mutation's id variable. It resolves to a session token
// for the Nango frontend SDK's Connect UI. Nothing changes until the caller
// completes that UI, so there's nothing to invalidate here.
export const startMcpAuthorizationMutationOptions = (
  authManager: AuthManager,
  org: DidString,
) =>
  mutationOptions({
    mutationFn: async (id: string) => {
      const response = await xrpc(
        pearAgent(authManager, `${org}#habitat`),
        network.habitat.mcp.startAuthorization.main,
        { body: { org, id } },
      );
      return response.body.sessionToken;
    },
  });

// disconnectMcpServerMutationOptions removes the caller's stored credential
// for an org-configured MCP server, identified by the mutation's id
// variable.
export const disconnectMcpServerMutationOptions = (
  authManager: AuthManager,
  org: DidString,
) =>
  mutationOptions({
    mutationFn: async (id: string, { client }) => {
      await xrpc(
        pearAgent(authManager, `${org}#habitat`),
        network.habitat.mcp.disconnectServer.main,
        { body: { org, id } },
      );
      await client.invalidateQueries({ queryKey: mcpServersQueryKey(org) });
    },
  });
