import { AuthManager } from "internal/authManager.js";

export interface CliqueRefPermission {
  $type: "network.habitat.grantee#cliqueRef";
  uri: string;
}

export interface DidGranteePermission {
  $type: "network.habitat.grantee#didGrantee";
  did: string;
}

export type Permission =
  | CliqueRefPermission
  | DidGranteePermission
  | { $type: string };

export type PostVisibility = "public" | "followers-only" | "specific-users";

export interface PrivatePostRecord {
  text: string;
  createdAt?: string;
  reply?: {
    parent: { uri: string };
    root: { uri: string };
  };
}

export interface PrivatePost {
  uri: string;
  cid: string;
  value: PrivatePostRecord;
  permissions?: Permission[];
}

export function getPostVisibility(
  post: PrivatePost,
  authorDid: string,
): PostVisibility {
  const perms = post.permissions;
  if (!perms || perms.length === 0) return "public";
  if (perms.length === 1) {
    const perm = perms[0];
    if (perm.$type === "network.habitat.grantee#cliqueRef") {
      const followersClique = `habitat://${authorDid}/network.habitat.clique/followers`;
      if ((perm as CliqueRefPermission).uri === followersClique)
        return "followers-only";
    }
  }
  return "specific-users";
}

export interface Profile {
  did: string;
  handle: string;
  displayName?: string;
  avatar?: string;
}

async function getCliqueMembers(
  authManager: AuthManager,
  cliqueUri: string,
): Promise<string[]> {
  // Parse habitat://did/network.habitat.clique/rkey
  const withoutScheme = cliqueUri.replace("habitat://", "");
  const parts = withoutScheme.split("/");
  const repo = parts[0];
  const rkey = parts[parts.length - 1];
  const collection = parts.slice(1, -1).join("/");

  const params = new URLSearchParams();
  params.append("repo", repo!);
  params.append("collection", collection);
  params.append("rkey", rkey!);
  params.append("includePermissions", "true");

  const response = await authManager.fetch(
    `/xrpc/network.habitat.getRecord?${params}`,
    "GET",
  );
  if (!response.ok) return [];
  const data: { permissions?: Permission[] } = await response.json();
  const res = (data.permissions ?? [])
    .filter(
      (p): p is DidGranteePermission =>
        p.$type === "network.habitat.grantee#didGrantee",
    )
    .map((p) => p.did);

  console.log("getCLiqueMembers", { cliqueUri, res });
  return res
}

export async function getPrivatePosts(
  authManager: AuthManager,
  handle?: string,
): Promise<PrivatePost[]> {
  const params = new URLSearchParams();
  params.append("collection", "app.bsky.feed.post");
  if (handle) {
    params.append("subjects", handle);
  }
  params.append("includePermissions", "true");

  // TODO: use habitat client api
  const response = await authManager.fetch(
    `/xrpc/network.habitat.listRecords?${params}`,
    "GET",
  );
  const data: { records?: PrivatePost[] } = await response.json();
  const posts = data.records ?? [];

  return Promise.all(
    posts.map(async (post) => {
      const perms = post.permissions ?? [];
      const authorDid = post.uri.split("/")[2] ?? "";
      const followersClique = `habitat://${authorDid}/network.habitat.clique/followers`;
      const nonFollowersCliqueRef = perms.find(
        (p): p is CliqueRefPermission =>
          p.$type === "network.habitat.grantee#cliqueRef" &&
          (p as CliqueRefPermission).uri !== followersClique,
      );
      if (nonFollowersCliqueRef) {
        const memberDids = await getCliqueMembers(
          authManager,
          nonFollowersCliqueRef.uri,
        );
        return {
          ...post,
          permissions: memberDids.map((did) => ({
            $type: "network.habitat.grantee#didGrantee" as const,
            did,
          })),
        };
      }
      return post;
    }),
  );
}

export async function getProfiles(
  authManager: AuthManager,
  actors: string[],
): Promise<{ avatar?: string; handle: string }[]> {
  if (actors.length === 0) return [];
  const params = new URLSearchParams();
  for (const actor of actors) {
    params.append("actors", actor);
  }
  const headers = new Headers();
  headers.append("at-proxy", "did:web:api.bsky.app#bsky_appview");
  const response = await authManager.fetch(
    `/xrpc/app.bsky.actor.getProfiles?${params.toString()}`,
    "GET",
    null,
    headers,
  );
  if (!response.ok) return [];
  const data: { profiles: Profile[] } = await response.json();
  return data.profiles.map((p) => ({ avatar: p.avatar, handle: p.handle }));
}

export async function getProfile(
  authManager: AuthManager,
  actor: string,
): Promise<Profile> {
  const headers = new Headers();
  headers.append("at-proxy", "did:web:api.bsky.app#bsky_appview");
  const params = new URLSearchParams();
  params.append("actor", actor);
  const response = await authManager.fetch(
    `/xrpc/app.bsky.actor.getProfile?${params.toString()}`,
    "GET",
    null,
    headers,
  );
  return response.json();
}
