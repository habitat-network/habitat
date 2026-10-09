import type { AuthManager } from "internal";
import { createSpaceSignatureHeaders, resolveSpaceHost } from "internal";
import { xrpc, type Agent, type SpaceRefString } from "@atproto/lex";
import { SpaceRef } from "@atproto/syntax";
import { queryOptions } from "@tanstack/react-query";
import { com } from "api";

export interface SpaceCredential {
  // The signed space credential, sent as a bearer token to `host`.
  credential: string;
  // The space authority's host, resolved from its DID document (the
  // atproto_space_host service, falling back to its PDS) — see
  // internal/utils.SpaceHostEndpoint on the Go side for the same rule. Every
  // com.atproto.space.* read for this space is served here, per
  // bluesky-social/proposals#0016.
  host: string;
  // The space authority's DID, signed as the audience on every request that
  // presents the credential.
  audience: string;
}

// spaceCredentialQueryOptions fetches a credential for reading `space`
// cross-repo: a delegation token minted under the caller's own OAuth session,
// exchanged for a credential the space owner's own key signs off on, against
// the space's own resolved host (not necessarily this pear instance). Cached
// per space, so any query that needs to read records out of the same space
// shares one exchange instead of repeating it.
export function spaceCredentialQueryOptions(
  space: string,
  authManager: AuthManager,
) {
  return queryOptions({
    queryKey: ["spaceCredential", space],
    queryFn: async (): Promise<SpaceCredential> => {
      const response = await xrpc(
        authManager,
        com.atproto.space.getDelegationToken.main,
        { params: { space: space as SpaceRefString } },
      );
      const { token: delegationToken } = response.body;
      const host = await resolveSpaceHost(SpaceRef.parse(space).spaceDid);
      // The atproto spaces protocol requires an HTTP message signature over
      // the delegation token, made by the key to bind the minted credential
      // to (see lexicons/com/atproto/space/getSpaceCredential.json), so a
      // spaces-capable PDS talked to directly (bypassing pear's proxy)
      // rejects this call without one. No credential exists yet at this
      // exchange step, so no audience is signed — the signature's keyid
      // carries the confirmation key instead.
      const {
        body: { credential },
      } = await xrpc(
        signedAgent(host, delegationToken),
        com.atproto.space.getSpaceCredential.main,
        { body: { space: space as SpaceRefString } },
      );
      return {
        credential,
        host,
        audience: SpaceRef.parse(space).spaceDid,
      };
    },
    // Credentials are short-lived, server-signed tokens; treat as fresh for a
    // few minutes instead of re-exchanging on every read of the space.
    staleTime: 2 * 60 * 1000,
  });
}

// spaceCredentialHeaders builds the Authorization + signature headers needed
// to present cred on a request. A space credential reads a whole space and is
// shown to every repo host in it — as a bearer token it would be a shared
// secret, since any host given one could replay it against every other host in
// the space. So per the permissioned-data proposal
// (github.com/bluesky-social/proposals/0016-permissioned-data), every request
// carries a fresh HTTP message signature over the credential and the audience
// DID it is addressed to, made by the key the credential is bound to.
export async function spaceCredentialHeaders(
  cred: SpaceCredential,
): Promise<HeadersInit> {
  return createSpaceSignatureHeaders(
    `Bearer ${cred.credential}`,
    cred.audience,
  );
}

// signedAgent is an xrpc Agent that sends requests to `host` with `token` as
// the bearer, adding a fresh space signature over it per request (and over
// `audience`, when given) instead of using the caller's own OAuth session.
function signedAgent(host: string, token: string, audience?: string): Agent {
  return {
    fetchHandler: async (path, init) => {
      const headers = new Headers(init.headers);
      const sigHeaders = await createSpaceSignatureHeaders(
        `Bearer ${token}`,
        audience,
      );
      for (const [key, value] of Object.entries(sigHeaders)) {
        headers.set(key, value);
      }
      return fetch(`${host}${path}`, { ...init, headers });
    },
  };
}

// spaceAgent turns a space credential into an xrpc Agent: requests are sent
// to the space's own resolved host (not this pear instance), signing
// the credential per request, so a lexicon-typed, lex-decoded read (e.g.
// com.atproto.space.listRecords) can be made the same way an
// authManager-backed one would.
export function spaceAgent(cred: SpaceCredential): Agent {
  return signedAgent(cred.host, cred.credential, cred.audience);
}
