import { Actor } from "./types/Actor";

const PUBLIC_BSKY_API = "https://public.api.bsky.app";

export async function searchActorsTypeahead(
  q: string,
  limit = 8,
): Promise<Actor[]> {
  const params = new URLSearchParams({ q, limit: String(limit) });
  const res = await fetch(
    `${PUBLIC_BSKY_API}/xrpc/app.bsky.actor.searchActorsTypeahead?${params}`,
  );
  const data: { actors: Actor[] } = await res.json();
  return data.actors ?? [];
}

export async function getProfiles(dids: string[]): Promise<Actor[]> {
  if (dids.length === 0) return [];
  const params = new URLSearchParams();
  dids.forEach((d) => params.append("actors", d));
  const res = await fetch(
    `${PUBLIC_BSKY_API}/xrpc/app.bsky.actor.getProfiles?${params}`,
  );
  const { profiles } = await res.json();
  return profiles ?? [];
}

export async function getProfile(did: string): Promise<Actor> {
  const params = new URLSearchParams({ actor: did });
  const res = await fetch(
    `${PUBLIC_BSKY_API}/xrpc/app.bsky.actor.getProfile?${params}`,
  );
  return res.json();
}
