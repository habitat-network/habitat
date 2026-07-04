// skeyOf extracts a space's skey (its last path segment) from a space URI like
// ats://<orgDid>/network.habitat.group/<skey>. Used as the clean route param
// for a group detail page.
export function skeyOf(uri: string): string {
  const parts = uri.split("/");
  return parts[parts.length - 1];
}

// groupUri reconstructs a group-space URI from the org DID and skey.
export function groupUri(orgDid: string, skey: string): string {
  return `ats://${orgDid}/network.habitat.group/${skey}`;
}

// displayDid renders a DID as a friendly handle: it prefers a known handle, then
// falls back to the did:web host (which is the member's handle), then the raw DID.
export function displayDid(did: string, handles?: Map<string, string>): string {
  const handle = handles?.get(did);
  if (handle) return handle;
  if (did.startsWith("did:web:")) {
    return decodeURIComponent(did.slice("did:web:".length));
  }
  return did;
}
