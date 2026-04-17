import { queryOptions } from "@tanstack/react-query";
import { AuthManager } from "./authManager";
import { query, procedure } from "./habitatClient";

export interface HabitatConfig {
  orgDomain: string | null;
}

export function getConfigQueryOptions(habitatDomain: string) {
  return queryOptions({
    queryKey: ["config"],
    queryFn: async (): Promise<HabitatConfig> => {
      const response = await fetch(
        `https://${habitatDomain}/xrpc/network.habitat.getConfig`,
      );
      return response.json();
    },
    staleTime: Infinity,
  });
}

export function getAdminsQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["org", "admins"],
    queryFn: () => query("network.habitat.org.getAdmins", {}, { authManager }),
  });
}

export function getMembersQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["org", "members"],
    queryFn: () =>
      query("network.habitat.org.getMembers", {}, { authManager }),
  });
}

export function addAdmin(authManager: AuthManager, admin: string) {
  return procedure("network.habitat.org.addAdmin", { admin }, { authManager });
}

export function addMembers(authManager: AuthManager, members: string[]) {
  return procedure(
    "network.habitat.org.addMembers",
    { members },
    { authManager },
  );
}

export function removeAdmin(authManager: AuthManager, admin: string) {
  return procedure(
    "network.habitat.org.removeAdmin",
    { admin },
    { authManager },
  );
}

export function removeMembers(authManager: AuthManager, members: string[]) {
  return procedure(
    "network.habitat.org.removeMembers",
    { members },
    { authManager },
  );
}

export function downgradeAdmin(authManager: AuthManager, admin: string) {
  return procedure(
    "network.habitat.org.downgradeAdmin",
    { admin },
    { authManager },
  );
}
