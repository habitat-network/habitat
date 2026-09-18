import type { AuthManager } from "internal";
import { createDpopProof, resolveSpaceHost } from "internal";
import { xrpc, type AgentConfig, type SpaceRefString } from "@atproto/lex";
import { SpaceRef } from "@atproto/syntax";
import { queryOptions } from "@tanstack/react-query";
import { com } from "api";

// fetchWithBearer makes a JSON request against `host` using an arbitrary
// bearer token (a delegation token or space credential) rather than the
// caller's own OAuth session. Used for the credential exchange itself and for
// endpoints (like getBlob) that don't return lex-typed JSON.
export async function fetchWithBearer(
  host: string,
  path: string,
  token: string,
  init?: RequestInit,
) {
  const res = await fetch(`${host}${path}`, {
    ...init,
    headers: { ...init?.headers, Authorization: `Bearer ${token}` },
  });
  const body = await res.json().catch(() => undefined);
  if (!res.ok) {
    throw new Error(
      body?.message || body?.error || `request failed: ${res.status}`,
    );
  }
  return body;
}

export interface SpaceCredential {
  // The signed space credential, sent as a bearer token to `host`.
  credential: string;
  // The space authority's host, resolved from its DID document (the
  // atproto_space_host service, falling back to its PDS) — see
  // internal/utils.SpaceHostEndpoint on the Go side for the same rule. Every
  // com.atproto.space.* read for this space is served here, per
  // bluesky-social/proposals#0016.
  host: string;
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
      const path = "/xrpc/com.atproto.space.getSpaceCredential";
      // The atproto spaces protocol requires a DPoP proof binding the minted
      // credential to a key the caller holds (see
      // lexicons/com/atproto/space/getSpaceCredential.json), so a
      // spaces-capable PDS talked to directly (bypassing pear's proxy)
      // rejects this call without one.
      const dpopProof = await createDpopProof(
        "POST",
        `${host}${path}`,
        delegationToken,
      );
      const { credential } = await fetchWithBearer(
        host,
        path,
        delegationToken,
        {
          method: "POST",
          headers: { "Content-Type": "application/json", DPoP: dpopProof },
          body: JSON.stringify({ space }),
        },
      );
      return { credential: credential as string, host };
    },
    // Credentials are short-lived, server-signed tokens; treat as fresh for a
    // few minutes instead of re-exchanging on every read of the space.
    staleTime: 2 * 60 * 1000,
  });
}

// spaceAgent turns a space credential into xrpc agent options: requests are
// sent to the space's own resolved host (not this pear instance) with the
// credential as a bearer token, so a lexicon-typed, lex-decoded read (e.g.
// com.atproto.space.listRecords) can be made the same way an
// authManager-backed one would.
export function spaceAgent(cred: SpaceCredential): AgentConfig {
  return {
    service: cred.host,
    headers: { Authorization: `Bearer ${cred.credential}` },
  };
}
