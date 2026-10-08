import { P256Keypair } from "@atproto/crypto";
import { createSpaceSigHeaders } from "@atproto/space";

// One ephemeral P-256 keypair per page load. Space credentials are bound to a
// did:key confirmation key the caller controls — nothing depends on this key
// surviving a reload, so there's no need to persist or reuse one across
// sessions.
let keyPromise: Promise<P256Keypair> | undefined;

function signingKey(): Promise<P256Keypair> {
  if (!keyPromise) {
    keyPromise = P256Keypair.create();
  }
  return keyPromise;
}

// createSpaceSignatureHeaders builds the authorization, signature-input and
// signature headers (plus atproto-space-audience when `audience` is given) for
// a space request, signing with this page's ephemeral key. Delegates to
// @atproto/space's createSpaceSigHeaders — the same implementation a space
// authority verifies against — rather than reimplementing the signature
// shape here. When exchanging a delegation token for a credential, omit
// `audience`: the signature then carries the key's did:key as `keyid`, which
// binds the minted credential to it. When presenting an already-minted
// credential, pass the audience DID being addressed and sign the credential
// itself, per the permissioned-data proposal
// (github.com/bluesky-social/proposals/0016-permissioned-data).
export async function createSpaceSignatureHeaders(
  authorization: string,
  audience?: string,
): Promise<Record<string, string>> {
  const key = await signingKey();
  return createSpaceSigHeaders(key, {
    authorization,
    audience: audience as `did:${string}:${string}` | undefined,
  });
}
