import type { AuthManager } from "internal";
import { agentFor } from "internal";
import { xrpc } from "@atproto/lex";
import { queryOptions } from "@tanstack/react-query";
import { network } from "api";

export function listPermissions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["permissions"],
    queryFn: async () => {
      const response = await xrpc(
        agentFor(authManager),
        network.habitat.permissions.listPermissions.main,
        { params: {} },
      );
      return response.body;
    },
  });
}