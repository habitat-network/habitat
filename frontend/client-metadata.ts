import type { OAuthClientMetadata } from "@atproto/oauth-client-browser";

export default (domain: string) =>
  ({
    client_id: `https://${domain}/client-metadata.json`,
    client_name: "Habitat",
    client_uri: `https://${domain}`,
    redirect_uris: [`https://${domain}/oauth-login`],
    scope: "atproto transition:generic",
    grant_types: ["authorization_code", "refresh_token"],
    response_types: ["code"],
    token_endpoint_auth_method: "none",
    application_type: "web",
    dpop_bound_access_tokens: true,
  }) satisfies Omit<
    OAuthClientMetadata,
    "subject_type" | "authorization_signed_response_alg"
  >;
