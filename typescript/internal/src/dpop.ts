import { WebcryptoKey } from "@atproto/jwk-webcrypto";
import { createDpopProof as createSpaceDpopProof } from "@atproto/space";

// One ephemeral DPoP keypair per page load. A DPoP proof only needs to prove
// possession of *some* key the caller controls — nothing depends on this key
// surviving a reload, so there's no need to persist or reuse one across
// sessions.
let keyPromise: Promise<WebcryptoKey> | undefined;

function dpopKey(): Promise<WebcryptoKey> {
  if (!keyPromise) {
    keyPromise = WebcryptoKey.generate(["ES256"]);
  }
  return keyPromise;
}

// createDpopProof builds a DPoP proof JWT (RFC 9449) for `method url`,
// binding it to this page's ephemeral key. Delegates to @atproto/space's
// createDpopProof — the same implementation a space authority verifies
// against — rather than reimplementing the proof shape here. When presenting
// an already-minted space credential (as opposed to exchanging a delegation
// token for one), pass it as `credential` so its hash is bound into the
// proof's "ath" claim, per the permissioned-data proposal's DPoP
// requirements (github.com/bluesky-social/proposals/0016-permissioned-data).
export async function createDpopProof(
  method: string,
  url: string,
  credential?: string,
): Promise<string> {
  const key = await dpopKey();
  return createSpaceDpopProof(key, { htm: method, htu: url, credential });
}
