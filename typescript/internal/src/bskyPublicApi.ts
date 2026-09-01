import { Actor } from "./types/Actor";

const PUBLIC_BSKY_API = "https://public.api.bsky.app";

export async function searchActorsTypeahead(
  q: string,
  { identityResolverUrl, limit = 8 }: { limit?: number; identityResolverUrl?: string } = {},
): Promise<Actor[]> {
  const params = new URLSearchParams({ q, limit: String(limit) });
  const [searchResp, resolveResp] = await Promise.all([
    fetch(
      `${PUBLIC_BSKY_API}/xrpc/app.bsky.actor.searchActorsTypeahead?${params}`,
    ),
    fetch(
      `${identityResolverUrl || "https://pear.habitat.network/xrpc/com.atproto.identity.resolveIdentity"}?identifier=${q}`,
    ),
  ]);
  const searchData: { actors: Actor[] } = await searchResp.json();
  if (resolveResp.ok) {
    const { did, handle } = await resolveResp.json();
    if (did) {
      searchData.actors.push({ did, handle });
    }
  }
  return searchData.actors ?? [];
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
  if (res.status !== 200) {
    return { did };
  }
  return res.json();
}
