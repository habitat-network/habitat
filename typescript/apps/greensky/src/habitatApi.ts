import { AuthManager, query } from "internal";

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
  clique?: string;
  resolvedClique?: string[];
}

export function getPostVisibility(
  post: PrivatePost,
  authorDid: string,
): PostVisibility {
  const perms = post.permissions;
  if (!perms || perms.length === 0) return "public";
  const followersClique = `habitat://${authorDid}/network.habitat.clique/followers`;

  // The get for the record can return many grants (for e.g. if the author gave permissions to a user for the whole collection)
  // To know if this was a followers post, see if the auther shared this with their followers clique
  if (
    perms.some((perm) => {
      if (perm.$type === "network.habitat.grantee#cliqueRef") {
        if ((perm as CliqueRefPermission).uri === followersClique) return true;
      }
      return false;
    })
  ) {
    return "followers-only";
  }
  return "specific-users";
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

  let data: { permissions?: Permission[] };
  try {
    data = await query(
      "network.habitat.repo.getRecord",
      { repo: repo!, collection, rkey: rkey!, includePermissions: true },
      { authManager },
    );
  } catch {
    return [];
  }
  const res = (data.permissions ?? [])
    .filter(
      (p): p is DidGranteePermission =>
        p.$type === "network.habitat.grantee#didGrantee",
    )
    .map((p) => p.did);

  return res;
}

async function resolvePostPermissions(
  authManager: AuthManager,
  post: PrivatePost,
): Promise<PrivatePost> {
  const perms = post.permissions ?? [];
  const authorDid = post.uri.split("/")[2] ?? "";
  const followersClique = `habitat://${authorDid}/network.habitat.clique/followers`;
  const cliqueRef = perms.find(
    (p): p is CliqueRefPermission =>
      p.$type === "network.habitat.grantee#cliqueRef",
  );
  if (!cliqueRef) return post;

  if (cliqueRef.uri === followersClique) {
    return { ...post, clique: cliqueRef.uri };
  }

  const memberDids = await getCliqueMembers(authManager, cliqueRef.uri);
  return { ...post, clique: cliqueRef.uri, resolvedClique: memberDids };
}

export async function getPrivatePosts(
  authManager: AuthManager,
  handle?: string,
): Promise<PrivatePost[]> {
  const response = await query(
    "network.habitat.repo.listRecords",
    {
      collection: "app.bsky.feed.post",
      subjects: handle ? [handle] : [],
      includePermissions: true,
    },
    { authManager },
  );
  const posts = (response.records ?? []) as unknown as PrivatePost[];

  return Promise.all(
    posts.map((post) => resolvePostPermissions(authManager, post)),
  );
}

export async function getPrivatePost(
  authManager: AuthManager,
  repo: string,
  rkey: string,
): Promise<PrivatePost | null> {
  let post: PrivatePost;
  try {
    post = await query(
      "network.habitat.repo.getRecord",
      { repo, collection: "app.bsky.feed.post", rkey, includePermissions: true },
      { authManager },
    ) as unknown as PrivatePost;
  } catch {
    return null;
  }
  return resolvePostPermissions(authManager, post);
}

