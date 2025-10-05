import {  habitatProxy } from "@/constants";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import { queryOptions } from "@tanstack/react-query";

export function listPermissions(session?: OAuthSession) {
  return queryOptions({
    queryKey: ["permissions"],
    queryFn: async () => {
      const response = await session?.fetchHandler(`/xrpc/com.habitat.listPermissions`, {
        headers: {
          'atproto-proxy': habitatProxy,
        }
      })
      const json: Record<string, string[]> = await response?.json();
      return json
    },
  })
}
