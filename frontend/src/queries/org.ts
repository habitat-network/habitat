import type { AuthManager } from "internal";
import { agentFor } from "internal";
import { xrpc, type DidString } from "@atproto/lex";
import { queryOptions } from "@tanstack/react-query";
import { network } from "api";

export interface HabitatConfig {
  orgDomain: string | null;
}

export function getConfigQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["config"],
    queryFn: async () => {
      const response = await xrpc(
        agentFor(authManager),
        network.habitat.org.getMetadata.main,
        { params: {} },
      );
      return response.body;
    },
    staleTime: Infinity,
  });
}

export function getAdminsQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["org", "admins"],
    queryFn: async () => {
      const response = await xrpc(
        agentFor(authManager),
        network.habitat.org.getAdmins.main,
        { params: {} },
      );
      return response.body;
    },
  });
}

export function getMembersQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["org", "members"],
    queryFn: async () => {
      const response = await xrpc(
        agentFor(authManager),
        network.habitat.org.getMembers.main,
        { params: {} },
      );
      return response.body;
    },
  });
}

export async function addAdmin(authManager: AuthManager, admin: string) {
  const response = await xrpc(
    agentFor(authManager),
    network.habitat.org.addAdmin.main,
    { body: { admin: admin as DidString } },
  );
  return response.body;
}

export async function addMembers(authManager: AuthManager, members: string[]) {
  const response = await xrpc(
    agentFor(authManager),
    network.habitat.org.addMembers.main,
    { body: { members: members as DidString[] } },
  );
  return response.body;
}

export async function removeAdmin(authManager: AuthManager, admin: string) {
  const response = await xrpc(
    agentFor(authManager),
    network.habitat.org.removeAdmin.main,
    { body: { admin: admin as DidString } },
  );
  return response.body;
}

export async function removeMembers(
  authManager: AuthManager,
  members: string[],
) {
  const response = await xrpc(
    agentFor(authManager),
    network.habitat.org.removeMembers.main,
    { body: { members: members as DidString[] } },
  );
  return response.body;
}

export async function downgradeAdmin(authManager: AuthManager, admin: string) {
  const response = await xrpc(
    agentFor(authManager),
    network.habitat.org.downgradeAdmin.main,
    { body: { admin: admin as DidString } },
  );
  return response.body;
}

export async function issueInviteToken(authManager: AuthManager) {
  const response = await xrpc(
    agentFor(authManager),
    network.habitat.org.issueInviteToken.main,
    { body: { reusable: true } },
  );
  return response.body;
}