import type { OAuthClientMetadata } from "@atproto/oauth-client-browser";

// READ_SPACES_SCOPE requests read access to every space (any type, any
// authority) per the permissioned-data proposal
// (github.com/bluesky-social/proposals, 0016-permissioned-data#oauth-scopes).
export const READ_SPACES_SCOPE =
  "atproto transition:generic space:*?authority=*&action=read";

// MANAGE_SPACES_SCOPE also grants writes to any collection in any space and
// creating, updating and deleting spaces (`manage`), for a client that manages
// a user's spaces. A write or createSpace on a PDS that enforces space scopes
// (such as the spaces alpha PDS) needs it; read-only `action=read` grants none.
export const MANAGE_SPACES_SCOPE =
  "atproto transition:generic space:*?authority=*&collection=*&manage=create&manage=update&manage=delete";

export default (
  clientName: string,
  baseUrl: string,
  scope: string = READ_SPACES_SCOPE,
) => {
  let origin = baseUrl;
  while (origin.endsWith("/")) origin = origin.slice(0, -1);
  return {
    client_id: `${origin}/client-metadata.json`,
    client_name: clientName,
    client_uri: origin,
    redirect_uris: [`${origin}/oauth-login`, origin],
    scope,
    grant_types: ["authorization_code", "refresh_token"],
    response_types: ["code"],
    token_endpoint_auth_method: "none",
    application_type: "web",
    dpop_bound_access_tokens: true,
    logo_uri: `${origin}/habitat.png`,
  } as Omit<
    OAuthClientMetadata,
    "subject_type" | "authorization_signed_response_alg"
  >;
};
