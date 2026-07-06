import { HandleResolver, DidResolver } from "@atproto/identity";

// Resolvers over the public AT Protocol identity system (the PLC directory for
// did:plc and .well-known for did:web). Instantiated once and reused so repeated
// lookups share any internal caching.
const handleResolver = new HandleResolver();
const didResolver = new DidResolver({});

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
