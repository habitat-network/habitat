import type { OAuthClientMetadata } from "@atproto/oauth-client-browser";

export default (clientName: string, baseUrl: string) => {
  const origin = baseUrl.replace(/\/+$/, "");
  return {
    client_id: `${origin}/client-metadata.json`,
    client_name: clientName,
    client_uri: origin,
    redirect_uris: [`${origin}/oauth-login`, origin],
    scope: "atproto transition:generic",
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
