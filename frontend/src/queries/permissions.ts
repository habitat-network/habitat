import type { AuthManager } from "@/auth";
import { queryOptions } from "@tanstack/react-query";

export function listPermissions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["permissions"],
    queryFn: async () => {
      const response = await authManager?.fetch(
        `/xrpc/com.habitat.listPermissions`,
      );
      const json: Record<string, string[]> = await response?.json();
      return json;
    },
  });
}
