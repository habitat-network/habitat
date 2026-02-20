import type { AuthManager } from "internal/authManager.ts";
import { queryOptions } from "@tanstack/react-query";

export interface Permission {
  grantee: string;
  collection: string;
  rkey: string;
  effect: string;
}

export function listPermissions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["permissions"],
    queryFn: async () => {
      const response = await authManager?.fetch(
        `/xrpc/network.habitat.listPermissions`,
      );
      const json: { permissions: Permission[] } = await response?.json();
      return json;
    },
  });
}
