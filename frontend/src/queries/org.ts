import type { AuthManager } from "internal";
import {
  xrpc,
  type DidString,
  type NsidString,
  type SpaceRefString,
} from "@atproto/lex";
import { SpaceRef } from "@atproto/syntax";
import { queryOptions } from "@tanstack/react-query";
import { com, network } from "api";

export interface HabitatConfig {
  orgDomain: string | null;
}

export function getConfigQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["config"],
    queryFn: async () => {
      const response = await xrpc(
        authManager,
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
        authManager,
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
        authManager,
        network.habitat.org.getMembers.main,
        { params: {} },
      );
      return response.body;
    },
  });
}

export async function addAdmin(authManager: AuthManager, admin: string) {
  const response = await xrpc(authManager, network.habitat.org.addAdmin.main, {
    body: { admin: admin as DidString },
  });
  return response.body;
}

export async function addMembers(authManager: AuthManager, members: string[]) {
  const response = await xrpc(
    authManager,
    network.habitat.org.addMembers.main,
    { body: { members: members as DidString[] } },
  );
  return response.body;
}

export async function removeAdmin(authManager: AuthManager, admin: string) {
  const response = await xrpc(
    authManager,
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
    authManager,
    network.habitat.org.removeMembers.main,
    { body: { members: members as DidString[] } },
  );
  return response.body;
}

export async function downgradeAdmin(authManager: AuthManager, admin: string) {
  const response = await xrpc(
    authManager,
    network.habitat.org.downgradeAdmin.main,
    { body: { admin: admin as DidString } },
  );
  return response.body;
}

export async function issueInviteToken(authManager: AuthManager) {
  const response = await xrpc(
    authManager,
    network.habitat.org.issueInviteToken.main,
    { body: { reusable: true } },
  );
  return response.body;
}

export interface MemberProfile {
  did: string;
  handle: string;
  displayName?: string;
  bio?: string;
  avatarUrl?: string;
}

// getProfileQueryOptions fetches a member's profile (handle plus their
// community.opensocial.memberProfile record, if any) within the caller's
// org. Any org member may fetch any other member's profile this way.
export function getProfileQueryOptions(authManager: AuthManager, did: string) {
  return queryOptions({
    queryKey: ["org", "profile", did],
    queryFn: async (): Promise<MemberProfile> => {
      const response = await xrpc(
        authManager,
        network.habitat.org.getProfile.main,
        { params: { did: did as DidString } },
      );
      return response.body;
    },
  });
}

// updateMemberProfile replaces the caller's own profile within org: display
// name, bio, and avatar URL. It's a self-authored
// community.opensocial.memberProfile record written directly into the org's
// members space, the same way a member authors their own acceptance record
// when joining (see acceptInvite) — no dedicated procedure is needed since
// members always configure their own profile.
export async function updateMemberProfile(
  authManager: AuthManager,
  org: string,
  displayName: string,
  bio: string,
  avatarUrl: string,
) {
  const did = authManager.getAuthInfo()!.did;
  const membersSpace = new SpaceRef(
    org as DidString,
    "community.opensocial.members",
    "self",
  ).toString();
  await xrpc(authManager, com.atproto.space.putRecord.main, {
    body: {
      space: membersSpace as SpaceRefString,
      repo: did as DidString,
      collection: "community.opensocial.memberProfile" as NsidString,
      rkey: did,
      record: {
        $type: "community.opensocial.memberProfile",
        displayName: displayName || undefined,
        bio: bio || undefined,
        avatarUrl: avatarUrl || undefined,
        updatedAt: new Date().toISOString(),
      },
    },
  });
}
