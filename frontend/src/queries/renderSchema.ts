import { queryOptions } from "@tanstack/react-query";
import { RENDER_SCHEMA_REGISTRY, type RenderSchema } from "@/lib/renderSchemas";

// Derive the authority domain from an NSID by reversing the first two segments.
// e.g. "community.lexicon.calendar.event" -> "lexicon.community"
//      "network.habitat.docs"             -> "habitat.network"
function nsidAuthority(nsid: string): string {
  const parts = nsid.split(".");
  return `${parts[1]}.${parts[0]}`;
}

async function fetchFromWellKnown(nsid: string): Promise<RenderSchema | null> {
  const authority = nsidAuthority(nsid);
  try {
    const res = await fetch(
      `https://${authority}/.well-known/habitat/render-schema?nsid=${encodeURIComponent(nsid)}`,
    );
    if (!res.ok) return null;
    return (await res.json()) as RenderSchema;
  } catch {
    return null;
  }
}

async function fetchFromAtProto(nsid: string): Promise<RenderSchema | null> {
  // Render schemas published by habitat are stored as records at:
  //   at://habitat.network/network.habitat.render.schema/{nsid}
  // We query the habitat XRPC endpoint directly since this is a public record.
  try {
    const params = new URLSearchParams({
      repo: "habitat.network",
      collection: "network.habitat.render.schema",
      rkey: nsid,
    });
    const res = await fetch(
      `/xrpc/network.habitat.repo.getRecord?${params.toString()}`,
    );
    if (!res.ok) return null;
    const json = await res.json();
    return json.value as RenderSchema;
  } catch {
    return null;
  }
}

export function renderSchemaQueryOptions(nsid: string) {
  return queryOptions({
    queryKey: ["renderSchema", nsid],
    staleTime: Infinity,
    queryFn: async (): Promise<RenderSchema | null> => {
      // 1. Static registry
      if (RENDER_SCHEMA_REGISTRY[nsid]) {
        return RENDER_SCHEMA_REGISTRY[nsid];
      }

      // 2. Well-known HTTP endpoint on the lexicon's authority domain
      const wellKnown = await fetchFromWellKnown(nsid);
      if (wellKnown) return wellKnown;

      // 3. AT Protocol record at habitat.network
      const atProto = await fetchFromAtProto(nsid);
      if (atProto) return atProto;

      return null;
    },
  });
}
