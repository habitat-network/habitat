import type { AuthManager } from "internal";
import { query, procedure, parseSpaceURI, constructSpaceURI } from "internal";
import { queryOptions, type QueryClient } from "@tanstack/react-query";
import type { InviteView } from "api/types/community/opensocial/defs";

export type { InviteView };

export interface MemberView {
  did: string;
  roles: string[];
}

const MEMBERS_SPACE_TYPE = "community.opensocial.members";

// OrgSummary is a community the calling user belongs to, resolved from the
// community.opensocial.members space they hold a repo in.
export interface OrgSummary {
  did: string;
  spaceUri: string;
}

// myOrgsQueryOptions lists the communities the calling user belongs to: every
// community.opensocial.members space they've written a membership or
// acceptance record into.
export function myOrgsQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["opensocial", "myOrgs"],
    queryFn: async (): Promise<OrgSummary[]> => {
      const { spaces } = await query(
        "network.habitat.space.listSpaces",
        { type: MEMBERS_SPACE_TYPE },
        { authManager },
      );
      const orgs: OrgSummary[] = [];
      for (const space of spaces) {
        const parts = parseSpaceURI(space.uri);
        if (parts) orgs.push({ did: parts.spaceOwner, spaceUri: space.uri });
      }
      return orgs;
    },
  });
}

// myInvitesQueryOptions lists the calling user's pending invites across every
// community on this instance.
export function myInvitesQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["opensocial", "myInvites"],
    queryFn: async (): Promise<InviteView[]> => {
      const { invites } = await query(
        "community.opensocial.listInvites",
        {},
        { authManager },
      );
      return invites;
    },
  });
}

// orgPendingInvitesQueryOptions lists a community's outstanding invites
// (across every invitee). Requires the caller to be an admin of the
// community.
export function orgPendingInvitesQueryOptions(
  org: string,
  authManager: AuthManager,
) {
  return queryOptions({
    queryKey: ["opensocial", "pendingInvites", org],
    queryFn: async (): Promise<InviteView[]> => {
      const { invites } = await query(
        "community.opensocial.listPendingInvites",
        { org },
        { authManager },
      );
      return invites;
    },
  });
}

// fetchWithBearer makes a JSON request against this pear instance using an
// arbitrary bearer token (a delegation token or space credential) rather than
// the caller's own OAuth session.
async function fetchWithBearer(
  path: string,
  token: string,
  init?: RequestInit,
) {
  const domain = import.meta.env.VITE_HABITAT_DOMAIN;
  const res = await fetch(`https://${domain}${path}`, {
    ...init,
    headers: { ...init?.headers, Authorization: `Bearer ${token}` },
  });
  const body = await res.json().catch(() => undefined);
  if (!res.ok) {
    throw new Error(
      body?.message || body?.error || `request failed: ${res.status}`,
    );
  }
  return body;
}

// spaceCredentialQueryOptions fetches a credential for reading `space`
// cross-repo: a delegation token minted under the caller's own OAuth session,
// exchanged for a credential the space owner's own key signs off on. Cached
// per space, so any query that needs to read records out of the same space
// (a profile, its avatar blob, ...) shares one exchange instead of repeating
// it.
export function spaceCredentialQueryOptions(
  space: string,
  authManager: AuthManager,
) {
  return queryOptions({
    queryKey: ["opensocial", "spaceCredential", space],
    queryFn: async (): Promise<string> => {
      const { token: delegationToken } = await query(
        "network.habitat.space.getDelegationToken",
        { space },
        { authManager },
      );
      const { credential } = await fetchWithBearer(
        "/xrpc/network.habitat.space.getSpaceCredential",
        delegationToken,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ space }),
        },
      );
      return credential as string;
    },
    // Credentials are short-lived, server-signed tokens; treat as fresh for a
    // few minutes instead of re-exchanging on every read of the space.
    staleTime: 2 * 60 * 1000,
  });
}

// The raw (un-jsonToLex'd) shape of a listRecords entry.
interface RawRecord {
  rkey: string;
  value?: { roles?: string[] };
}

// orgMembersQueryOptions lists a community's members and their roles, read
// directly off the community.opensocial.membership records in its members
// space via a space credential (see spaceCredentialQueryOptions) — there's no
// dedicated listMembers endpoint.
export function orgMembersQueryOptions(
  org: string,
  authManager: AuthManager,
  queryClient: QueryClient,
) {
  const membersSpace = constructSpaceURI({
    spaceOwner: org,
    spaceType: "community.opensocial.members",
    spaceKey: "self",
  });
  return queryOptions({
    queryKey: ["opensocial", "members", org],
    queryFn: async (): Promise<MemberView[]> => {
      const credential = await queryClient.fetchQuery(
        spaceCredentialQueryOptions(membersSpace, authManager),
      );
      const params = new URLSearchParams({
        space: membersSpace,
        repo: org,
        collection: "community.opensocial.membership",
      });
      const { records } = await fetchWithBearer(
        `/xrpc/network.habitat.space.listRecords?${params}`,
        credential,
      );
      return (records as RawRecord[]).map((record) => ({
        did: record.rkey,
        roles: record.value?.roles ?? [],
      }));
    },
  });
}

export interface OrgProfile {
  name: string;
  description?: string;
  avatarUrl?: string;
}

// The raw (un-jsonToLex'd) shape of a blob reference as it comes back from
// getRecord: {"$type":"blob","ref":{"$link":"<cid>"},"mimeType":"...","size":...}.
interface RawBlobRef {
  ref?: { $link?: string };
  mimeType?: string;
}

// orgProfileQueryOptions fetches a community's profile record (and its
// avatar image, if set). The about space isn't readable through the caller's
// own OAuth session (it belongs to the community's own repo, not the
// caller's), so it's read via a space credential (see
// spaceCredentialQueryOptions) the way a cross-instance reader would.
// Resolves to null if the community hasn't set a profile, or isn't
// reachable/readable.
export function orgProfileQueryOptions(
  org: string,
  authManager: AuthManager,
  queryClient: QueryClient,
) {
  const aboutSpace = constructSpaceURI({
    spaceOwner: org,
    spaceType: "community.opensocial.about",
    spaceKey: "self",
  });
  return queryOptions({
    queryKey: ["opensocial", "profile", org],
    queryFn: async (): Promise<OrgProfile | null> => {
      try {
        const credential = await queryClient.fetchQuery(
          spaceCredentialQueryOptions(aboutSpace, authManager),
        );
        const params = new URLSearchParams({
          space: aboutSpace,
          repo: org,
          collection: "community.opensocial.profile",
          rkey: "self",
        });
        const { value } = await fetchWithBearer(
          `/xrpc/network.habitat.space.getRecord?${params}`,
          credential,
        );
        const profile: OrgProfile = {
          name: value.name,
          description: value.description,
        };
        const avatar = value.avatar as RawBlobRef | undefined;
        const cid = avatar?.ref?.$link;
        if (cid) {
          const blobParams = new URLSearchParams({ space: aboutSpace, cid });
          const domain = import.meta.env.VITE_HABITAT_DOMAIN;
          const blobRes = await fetch(
            `https://${domain}/xrpc/network.habitat.space.getBlob?${blobParams}`,
            { headers: { Authorization: `Bearer ${credential}` } },
          );
          if (blobRes.ok) {
            profile.avatarUrl = URL.createObjectURL(await blobRes.blob());
          }
        }
        return profile;
      } catch {
        return null;
      }
    },
  });
}

// updateProfile replaces a community's profile name/description. Requires
// the caller to be an admin of the community.
export function updateProfile(
  authManager: AuthManager,
  org: string,
  name: string,
  description: string,
) {
  return procedure(
    "community.opensocial.updateProfile",
    { org, name, description: description || undefined },
    { authManager },
  );
}

// uploadOrgImage sets a community's profile avatar from raw image bytes.
// Requires the caller to be an admin of the community. Uses
// authManager.fetch directly, like other raw-body blob uploads, since this
// endpoint takes the image as its request body rather than JSON.
export async function uploadOrgImage(
  authManager: AuthManager,
  org: string,
  file: File,
) {
  const buf = await file.arrayBuffer();
  const headers = new Headers();
  headers.append("Content-Type", file.type || "application/octet-stream");
  const res = await authManager.fetch(
    `/xrpc/community.opensocial.uploadImage?org=${encodeURIComponent(org)}`,
    "POST",
    buf,
    headers,
  );
  if (!res) {
    throw new Error("Upload failed: no response");
  }
  if (!res.ok) {
    const body = await res.json().catch(() => undefined);
    throw new Error(
      body?.message || body?.error || `upload failed: ${res.status}`,
    );
  }
  return res.json();
}

// createOrg mints a new community and makes the caller its admin.
export function createOrg(authManager: AuthManager, handle: string) {
  return procedure(
    "network.habitat.opensocial.createOrg",
    { handle },
    { authManager },
  );
}

// acceptInvite consumes the caller's pending invite to `org`, granting the
// roles it carried, then writes the caller's own community.opensocial.acceptance
// record into the org's members space — that's the caller's explicit act of
// joining, so it's authored under their own credentials rather than by the
// backend on their behalf.
export async function acceptInvite(authManager: AuthManager, org: string) {
  const { roles } = await procedure(
    "community.opensocial.requestJoin",
    { org },
    { authManager },
  );
  const membersSpace = constructSpaceURI({
    spaceOwner: org,
    spaceType: "community.opensocial.members",
    spaceKey: "self",
  });
  await procedure(
    "network.habitat.space.putRecord",
    {
      space: membersSpace,
      repo: authManager.getAuthInfo()!.did,
      collection: "community.opensocial.acceptance",
      rkey: "self",
      record: {
        $type: "community.opensocial.acceptance",
        updatedAt: new Date().toISOString(),
      },
    },
    { authManager },
  );
  return { roles };
}

// createInvite invites `invitee` to join `org`, granting them `roles` once
// they accept. Requires the caller to be an admin of the community.
export function createInvite(
  authManager: AuthManager,
  org: string,
  invitee: string,
  roles: string[] = ["member"],
) {
  return procedure(
    "community.opensocial.createInvite",
    { org, invitee, roles },
    { authManager },
  );
}
