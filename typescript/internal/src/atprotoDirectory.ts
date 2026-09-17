import {
  HandleResolver,
  DidResolver,
  type DidDocument,
} from "@atproto/identity";

// Resolvers over the public AT Protocol identity system (the PLC directory for
// did:plc and .well-known for did:web). Instantiated once and reused so repeated
// lookups share any internal caching.
const handleResolver = new HandleResolver();
const didResolver = new DidResolver({});

// getServiceEndpoint looks up a service entry by id (a "#fragment", matched
// either bare or prefixed with the doc's own DID, per the DID core spec) and
// returns its endpoint if it's a plain string.
function getServiceEndpoint(doc: DidDocument, id: string): string | undefined {
  const service = doc.service?.find(
    (s) => s.id === id || s.id === `${doc.id}${id}`,
  );
  return typeof service?.serviceEndpoint === "string"
    ? service.serviceEndpoint
    : undefined;
}

// resolveHandleToDid looks a handle up in the atproto directory and returns its
// DID, throwing if the handle does not resolve.
export async function resolveHandleToDid(handle: string): Promise<string> {
  const trimmed = handle.trim().replace(/^@/, "");
  const did = await handleResolver.resolve(trimmed);
  if (!did) {
    throw new Error(`Handle not found: ${trimmed}`);
  }
  return did;
}

// resolveDidToHandle resolves a DID through the atproto directory and returns
// its handle, or undefined if it cannot be resolved. It reads the handle from
// the DID document's alsoKnownAs (the first at:// aka) rather than
// resolveAtprotoData, so it works for Habitat's did:web identities too — those
// expose a #habitat service instead of an atproto PDS, which resolveAtprotoData
// rejects.
export async function resolveDidToHandle(
  did: string,
): Promise<string | undefined> {
  try {
    const doc = await didResolver.resolve(did);
    const aka = doc?.alsoKnownAs?.find((a) => a.startsWith("at://"));
    return aka?.slice("at://".length);
  } catch {
    return undefined;
  }
}

// resolveSpaceHost resolves the host that serves a space's records and blobs
// (network.habitat.space.* — see bluesky-social/proposals#0016): the
// `atproto_space_host` service on the space owner's DID document when it
// declares one, falling back to its PDS endpoint. Throws if the DID can't be
// resolved or declares neither service.
export async function resolveSpaceHost(spaceOwnerDid: string): Promise<string> {
  const doc = await didResolver.resolve(spaceOwnerDid);
  if (!doc) {
    throw new Error(`DID not found: ${spaceOwnerDid}`);
  }
  const spaceHost = getServiceEndpoint(doc, "#atproto_space_host");
  if (spaceHost) {
    return spaceHost;
  }
  const pds = getServiceEndpoint(doc, "#atproto_pds");
  if (!pds) {
    throw new Error(`No space host or PDS found for DID: ${spaceOwnerDid}`);
  }
  return pds;
}
