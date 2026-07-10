import type { AuthManager } from "internal";
import { query, procedure } from "internal";
import { queryOptions } from "@tanstack/react-query";

export interface HabitatConfig {
  orgDomain: string | null;
}

export function getConfigQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["config"],
    queryFn: () =>
      query("network.habitat.org.getMetadata", {}, { fetcher: authManager }),
    staleTime: Infinity,
  });
}

export function getAdminsQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["org", "admins"],
    queryFn: () => query("network.habitat.org.getAdmins", {}, { fetcher: authManager }),
  });
}

export function getMembersQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["org", "members"],
    queryFn: () => query("network.habitat.org.getMembers", {}, { fetcher: authManager }),
  });
}

export function addAdmin(authManager: AuthManager, admin: string) {
  return procedure("network.habitat.org.addAdmin", { admin }, { fetcher: authManager });
}

export function addMembers(authManager: AuthManager, members: string[]) {
  return procedure(
    "network.habitat.org.addMembers",
    { members },
    { fetcher: authManager },
  );
}

export function removeAdmin(authManager: AuthManager, admin: string) {
  return procedure(
    "network.habitat.org.removeAdmin",
    { admin },
    { fetcher: authManager },
  );
}

export function removeMembers(authManager: AuthManager, members: string[]) {
  return procedure(
    "network.habitat.org.removeMembers",
    { members },
    { fetcher: authManager },
  );
}

export function downgradeAdmin(authManager: AuthManager, admin: string) {
  return procedure(
    "network.habitat.org.downgradeAdmin",
    { admin },
    { fetcher: authManager },
  );
}

export function issueInviteToken(authManager: AuthManager) {
  return procedure(
    "network.habitat.org.issueInviteToken",
    {
      reusable: true,
    },
    { fetcher: authManager },
  );
}
