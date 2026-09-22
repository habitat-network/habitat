import type { AuthManager } from "internal";
import { xrpc } from "@atproto/lex";
import { queryOptions } from "@tanstack/react-query";
import { network } from "api";

export function listMcpServersQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["mcp", "servers"],
    queryFn: async () => {
      const response = await xrpc(
        authManager,
        network.habitat.mcp.listServers.main,
        { params: {} },
      );
      return response.body;
    },
  });
}

export async function addMcpServer(
  authManager: AuthManager,
  input: network.habitat.mcp.addServer.$InputBody,
) {
  const response = await xrpc(
    authManager,
    network.habitat.mcp.addServer.main,
    { body: input },
  );
  return response.body;
}

export async function updateMcpServer(
  authManager: AuthManager,
  input: network.habitat.mcp.updateServer.$InputBody,
) {
  const response = await xrpc(
    authManager,
    network.habitat.mcp.updateServer.main,
    { body: input },
  );
  return response.body;
}

export async function removeMcpServer(authManager: AuthManager, id: string) {
  await xrpc(authManager, network.habitat.mcp.removeServer.main, {
    body: { id },
  });
}

export async function connectMcpServer(
  authManager: AuthManager,
  id: string,
  credential: string,
) {
  await xrpc(authManager, network.habitat.mcp.connectServer.main, {
    body: { id, credential },
  });
}

export async function disconnectMcpServer(
  authManager: AuthManager,
  id: string,
) {
  await xrpc(authManager, network.habitat.mcp.disconnectServer.main, {
    body: { id },
  });
}
