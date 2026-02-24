import { AuthManager } from "internal/authManager.js";

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
}

export interface Profile {
  handle: string;
  displayName?: string;
  avatar?: string;
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
  const response = await authManager.fetch(
    `/xrpc/network.habitat.listRecords?${params}`,
    "GET",
  );
  const data: { records?: PrivatePost[] } = await response.json();
  return data.records ?? [];
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
