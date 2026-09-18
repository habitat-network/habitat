import type { AuthManager } from "internal";
import {
  getBlobCidString,
  xrpc,
  type BlobRef,
  type DidString,
  type NsidString,
  type SpaceRefString,
} from "@atproto/lex";
import { SpaceRef, ensureValidDid } from "@atproto/syntax";
import { queryOptions, type QueryClient } from "@tanstack/react-query";
import { com, community, network } from "api";
import { fetchClientMetadata } from "@/lib/oauthScopes";
import { spaceAgent, spaceCredentialQueryOptions } from "./spaceCredential";

export type InviteView = community.opensocial.defs.InviteView;

export interface MemberView {
  did: string;
  roles: string[];
}

const MEMBERS_SPACE_TYPE = "community.opensocial.members";

// OrgSummary is a community the calling user belongs to, resolved from the
// community.opensocial.members space they hold a repo in.
export interface OrgSummary {
  did: DidString;
  spaceUri: string;
}

// myOrgsQueryOptions lists the communities the calling user belongs to: every
// community.opensocial.members space they've written a membership or
// acceptance record into.
export function myOrgsQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["opensocial", "myOrgs"],
    queryFn: async (): Promise<OrgSummary[]> => {
      const response = await xrpc(
        authManager,
        com.atproto.space.listSpaces.main,
        { params: { type: MEMBERS_SPACE_TYPE as NsidString } },
      );
      const orgs: OrgSummary[] = [];
      for (const space of response.body.spaces) {
        orgs.push({
          did: SpaceRef.parse(space.uri).spaceDid,
          spaceUri: space.uri,
        });
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
      const response = await xrpc(
        authManager,
        community.opensocial.listInvites.main,
        { params: {} },
      );
      return response.body.invites;
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
      const response = await xrpc(
        authManager,
        community.opensocial.listPendingInvites.main,
        { params: { org: org as DidString } },
      );
      return response.body.invites;
    },
  });
}

// orgMembersQueryOptions lists a community's members and their roles, read
// directly off the community.opensocial.membership records in its members
// space via a space credential (see spaceCredentialQueryOptions) — there's no
// dedicated listMembers endpoint.
export function orgMembersQueryOptions(
  org: DidString,
  authManager: AuthManager,
  queryClient: QueryClient,
) {
  const membersSpace = new SpaceRef(
    org,
    "community.opensocial.members",
    "self",
  ).toString();
  return queryOptions({
    queryKey: ["opensocial", "members", org],
    queryFn: async (): Promise<MemberView[]> => {
      const cred = await queryClient.fetchQuery(
        spaceCredentialQueryOptions(membersSpace, authManager),
      );
      const response = await xrpc(
        spaceAgent(cred),
        com.atproto.space.listRecords.main,
        {
          params: {
            space: membersSpace as SpaceRefString,
            repo: org,
            collection: "community.opensocial.membership" as NsidString,
          },
        },
      );
      return response.body.records.map((record) => ({
        did: record.rkey,
        roles: (record.value as { roles?: string[] } | undefined)?.roles ?? [],
      }));
    },
  });
}

export interface AppAccessView {
  clientId: string;
  // The record's rkey: a base64url (no padding) encoding of clientId (see
  // internal/syntax/app_access.go). Route params can't safely carry a raw
  // client_id (typically a URL, with slashes and colons), so app-detail
  // links use this opaque, already-URL-safe id instead and decode it back
  // with decodeAppAccessRkey.
  rkey: string;
  // The scopes granted to this client as of its most recent org-credential
  // approval (see internal/oauthserver's HandleToken). Empty if the grant
  // predates this field, or wasn't made through that flow.
  scopes: string[];
}

// decodeAppAccessRkey reverses AppAccessRkey (internal/syntax/app_access.go):
// the record key is the base64url (no padding) encoding of the client_id.
export function decodeAppAccessRkey(rkey: string): string {
  const base64 = rkey.replace(/-/g, "+").replace(/_/g, "/");
  return atob(base64);
}

// orgAppAccessQueryOptions lists a community's approved apps, read directly
// off the network.habitat.space.appAccess records the org repo owns in its
// members space via a space credential (see spaceCredentialQueryOptions) —
// there's no dedicated listAppAccess endpoint.
export function orgAppAccessQueryOptions(
  org: DidString,
  authManager: AuthManager,
  queryClient: QueryClient,
) {
  const membersSpace = new SpaceRef(
    org,
    "community.opensocial.members",
    "self",
  ).toString();
  return queryOptions({
    queryKey: ["opensocial", "appAccess", org],
    queryFn: async (): Promise<AppAccessView[]> => {
      const cred = await queryClient.fetchQuery(
        spaceCredentialQueryOptions(membersSpace, authManager),
      );
      const response = await xrpc(
        spaceAgent(cred),
        com.atproto.space.listRecords.main,
        {
          params: {
            space: membersSpace as SpaceRefString,
            repo: org,
            collection: "network.habitat.space.appAccess" as NsidString,
          },
        },
      );
      return response.body.records.map((record) => ({
        clientId: decodeAppAccessRkey(record.rkey),
        rkey: record.rkey,
        scopes:
          (record.value as { scopes?: string[] } | undefined)?.scopes ?? [],
      }));
    },
  });
}

// clientMetadataQueryOptions fetches (and caches) an authorized app's OAuth
// client metadata document, keyed only by client_id since the document is
// not org-specific.
export function clientMetadataQueryOptions(clientId: string) {
  return queryOptions({
    queryKey: ["opensocial", "clientMetadata", clientId],
    queryFn: () => fetchClientMetadata(clientId),
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
}

export interface OrgProfile {
  name: string;
  description?: string;
  avatarUrl?: string;
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
  ensureValidDid(org);
  const aboutSpace = new SpaceRef(
    org,
    "community.opensocial.about",
    "self",
  ).toString();
  return queryOptions({
    queryKey: ["opensocial", "profile", org],
    queryFn: async (): Promise<OrgProfile | null> => {
      try {
        const cred = await queryClient.fetchQuery(
          spaceCredentialQueryOptions(aboutSpace, authManager),
        );
        const agent = spaceAgent(cred);
        const response = await xrpc(agent, com.atproto.space.getRecord.main, {
          params: {
            space: aboutSpace as SpaceRefString,
            repo: org as DidString,
            collection: "community.opensocial.profile" as NsidString,
            rkey: "self",
          },
        });
        const value = response.body.value as {
          name: string;
          description?: string;
          avatar?: BlobRef;
        };
        const profile: OrgProfile = {
          name: value.name,
          description: value.description,
        };
        const cid = value.avatar && getBlobCidString(value.avatar);
        if (cid) {
          const blobParams = new URLSearchParams({ space: aboutSpace, cid });
          const blobRes = await fetch(
            `${cred.host}/xrpc/com.atproto.space.getBlob?${blobParams}`,
            { headers: { Authorization: `Bearer ${cred.credential}` } },
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
export async function updateProfile(
  authManager: AuthManager,
  org: string,
  name: string,
  description: string,
) {
  const response = await xrpc(
    authManager,
    community.opensocial.updateProfile.main,
    {
      body: {
        org: org as DidString,
        name,
        description: description || undefined,
      },
    },
  );
  return response.body;
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
export async function createOrg(authManager: AuthManager, handle: string) {
  const response = await xrpc(
    authManager,
    network.habitat.opensocial.createOrg.main,
    { body: { handle } },
  );
  return response.body;
}

// acceptInvite consumes the caller's pending invite to `org`, granting the
// roles it carried, then writes the caller's own community.opensocial.acceptance
// record into the org's members space — that's the caller's explicit act of
// joining, so it's authored under their own credentials rather than by the
// backend on their behalf.
export async function acceptInvite(authManager: AuthManager, org: string) {
  const response = await xrpc(
    authManager,
    community.opensocial.requestJoin.main,
    { body: { org: org as DidString } },
  );
  const { roles } = response.body;
  ensureValidDid(org);
  const membersSpace = new SpaceRef(
    org,
    "community.opensocial.members",
    "self",
  ).toString();
  await xrpc(authManager, com.atproto.space.putRecord.main, {
    body: {
      space: membersSpace as SpaceRefString,
      repo: authManager.getAuthInfo()!.did as DidString,
      collection: "community.opensocial.acceptance" as NsidString,
      rkey: "self",
      record: {
        $type: "community.opensocial.acceptance",
        updatedAt: new Date().toISOString(),
      },
    },
  });
  return { roles };
}

export interface RoleView {
  rkey: string;
  name: string;
  description?: string;
}

// orgRolesQueryOptions lists a community's declared roles, read directly off
// the community.opensocial.role records in its members space via a space
// credential (see spaceCredentialQueryOptions) — there's no dedicated
// listRoles endpoint.
export function orgRolesQueryOptions(
  org: DidString,
  authManager: AuthManager,
  queryClient: QueryClient,
) {
  const membersSpace = new SpaceRef(
    org,
    "community.opensocial.members",
    "self",
  ).toString();
  return queryOptions({
    queryKey: ["opensocial", "roles", org],
    queryFn: async (): Promise<RoleView[]> => {
      const cred = await queryClient.fetchQuery(
        spaceCredentialQueryOptions(membersSpace, authManager),
      );
      const response = await xrpc(
        spaceAgent(cred),
        com.atproto.space.listRecords.main,
        {
          params: {
            space: membersSpace as SpaceRefString,
            repo: org,
            collection: "community.opensocial.role" as NsidString,
          },
        },
      );
      return response.body.records.map((record) => {
        const value = record.value as
          { name?: string; description?: string } | undefined;
        return {
          rkey: record.rkey,
          name: value?.name ?? record.rkey,
          description: value?.description,
        };
      });
    },
  });
}

export type ActionBinding = community.opensocial.permissions.ActionBinding;
export type AssignableBinding =
  community.opensocial.permissions.AssignableBinding;

export interface PermissionsView {
  bindings: ActionBinding[];
  assignable: AssignableBinding[];
}

// orgPermissionsQueryOptions fetches a community's authz configuration (the
// community.opensocial.permissions record), read via a space credential.
// Resolves to empty bindings if the community hasn't written one yet.
export function orgPermissionsQueryOptions(
  org: DidString,
  authManager: AuthManager,
  queryClient: QueryClient,
) {
  const membersSpace = new SpaceRef(
    org,
    "community.opensocial.members",
    "self",
  ).toString();
  return queryOptions({
    queryKey: ["opensocial", "permissions", org],
    queryFn: async (): Promise<PermissionsView> => {
      const cred = await queryClient.fetchQuery(
        spaceCredentialQueryOptions(membersSpace, authManager),
      );
      try {
        const response = await xrpc(
          spaceAgent(cred),
          com.atproto.space.getRecord.main,
          {
            params: {
              space: membersSpace as SpaceRefString,
              repo: org,
              collection: "community.opensocial.permissions" as NsidString,
              rkey: "self",
            },
          },
        );
        const value = response.body.value as {
          bindings?: ActionBinding[];
          assignable?: AssignableBinding[];
        };
        return {
          bindings: value.bindings ?? [],
          assignable: value.assignable ?? [],
        };
      } catch {
        return { bindings: [], assignable: [] };
      }
    },
  });
}

// orgProxyHeaders routes an XRPC call through pear's Atproto-Proxy
// middleware (internal/forwarding.ServiceProxy): the caller's own OAuth
// session is validated, then the request is re-signed server-side as a
// service-auth JWT audienced to the org DID's #habitat service, which is
// what the service-auth-only endpoints below require. Used for every
// org-admin mutation that isn't already accepted over the caller's own
// OAuth session (see e.g. updateProfile/createInvite, which are).
function orgProxyHeaders(org: string): HeadersInit {
  return { "Atproto-Proxy": `${org}#habitat` };
}

// putRole creates or updates a role declaration. Requires the caller to hold
// the community.configure action.
export async function putRole(
  authManager: AuthManager,
  org: string,
  role: string,
  name: string,
  description?: string,
) {
  const response = await xrpc(authManager, community.opensocial.putRole.main, {
    headers: orgProxyHeaders(org),
    body: {
      org: org as DidString,
      role,
      name,
      description: description || undefined,
    },
  });
  return response.body;
}

// deleteRole removes a role declaration. Requires the caller to hold the
// community.configure action; the built-in admin/member roles can't be
// removed.
export async function deleteRole(
  authManager: AuthManager,
  org: string,
  role: string,
) {
  const response = await xrpc(
    authManager,
    community.opensocial.deleteRole.main,
    { headers: orgProxyHeaders(org), body: { org: org as DidString, role } },
  );
  return response.body;
}

// updatePermissions replaces a community's authz configuration: which roles
// authorize which actions, and which roles each role may assign/eject.
// Requires the caller to hold the community.configure action.
export async function updatePermissions(
  authManager: AuthManager,
  org: string,
  bindings: ActionBinding[],
  assignable: AssignableBinding[],
) {
  const response = await xrpc(
    authManager,
    community.opensocial.updatePermissions.main,
    {
      headers: orgProxyHeaders(org),
      body: { org: org as DidString, bindings, assignable },
    },
  );
  return response.body;
}

// assignRoles sets a member's full role set. Requires the caller to hold the
// role.assign action, bounded by the roles it's permitted to assign.
export async function assignRoles(
  authManager: AuthManager,
  org: string,
  member: string,
  roles: string[],
) {
  const response = await xrpc(
    authManager,
    community.opensocial.assignRoles.main,
    {
      headers: orgProxyHeaders(org),
      body: { org: org as DidString, member: member as DidString, roles },
    },
  );
  return response.body;
}

// ejectMember removes a member from the community, revoking their roles and
// access. Requires the caller to hold the eject action, bounded by the roles
// it's permitted to assign.
export async function ejectMember(
  authManager: AuthManager,
  org: string,
  member: string,
) {
  const response = await xrpc(
    authManager,
    community.opensocial.ejectMember.main,
    {
      headers: orgProxyHeaders(org),
      body: { org: org as DidString, member: member as DidString },
    },
  );
  return response.body;
}

// createInvite invites `invitee` to join `org`, granting them `roles` once
// they accept. Requires the caller to be an admin of the community.
export async function createInvite(
  authManager: AuthManager,
  org: string,
  invitee: string,
  roles: string[] = ["member"],
) {
  const response = await xrpc(
    authManager,
    community.opensocial.createInvite.main,
    {
      body: {
        org: org as DidString,
        invitee: invitee as DidString,
        roles,
      },
    },
  );
  return response.body;
}
