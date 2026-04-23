import type { AuthManager } from "internal";
import { query } from "internal";
import { queryOptions } from "@tanstack/react-query";

export function listPermissions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["permissions"],
    queryFn: () =>
      query("network.habitat.permissions.listPermissions", {}, { authManager }),
  });
}
