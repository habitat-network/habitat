import type { AuthManager } from "internal/authManager.ts";
import { queryOptions } from "@tanstack/react-query";
import { NetworkHabitatPermissionsListPermissions } from "api";

export function listPermissions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["permissions"],
    queryFn: async () => {
      const response = await authManager?.fetch(
        `/xrpc/network.habitat.listPermissions`,
      );
      const json: NetworkHabitatPermissionsListPermissions.OutputSchema =
        await response?.json();
      return json;
    },
  });
}
