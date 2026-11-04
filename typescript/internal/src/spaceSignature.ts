import { P256Keypair } from "@atproto/crypto";
import {
  COMMIT_MAC_TYPE,
  WANT_MAC_HEADER,
  createSpaceSigHeaders,
  formatWantMacHeader,
} from "@atproto/space";

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

// spaceCredentialKid is the did:key of this page's ephemeral key, sent as
// getSpaceCredential's credentialKid so the minted credential is bound to it.
export async function spaceCredentialKid(): Promise<string> {
  return (await signingKey()).did();
}

// createSpaceSignatureHeaders builds the authorization, atproto-space-audience,
// signature-input and signature headers for a request presenting a space
// credential, signing with this page's ephemeral key. Delegates to
// @atproto/space's createSpaceSigHeaders — the same implementation a space
// authority verifies against — rather than reimplementing the signature
// shape here. `authorization` must use the Atproto-Space-Credential scheme,
// and `audience` is the DID being addressed, per the permissioned-data
// proposal (github.com/bluesky-social/proposals/0016-permissioned-data).
export async function createSpaceSignatureHeaders(
  authorization: string,
  audience: string,
): Promise<Record<string, string>> {
  const key = await signingKey();
  return createSpaceSigHeaders(key, {
    authorization,
    audience: audience as `did:${string}:${string}`,
  });
}

// wantCommitMacHeaders builds the Atproto-Space-Want-Mac header a repo host
// requires on commit reads made with a space credential (getLatestCommit,
// listRepoOps, getRepo). The key share is a fresh ephemeral P-256 public key
// whose private half is discarded: this is for displaying a commit, not
// syncing it, so the response's MAC is never checked. A syncer verifying the
// commit encapsulates against the author's #atproto_kem key instead (see
// spaceSync's SpaceClient).
export async function wantCommitMacHeaders(): Promise<Record<string, string>> {
  const ephemeral = await P256Keypair.create();
  return {
    [WANT_MAC_HEADER]: formatWantMacHeader(COMMIT_MAC_TYPE, ephemeral.did()),
  };
}
