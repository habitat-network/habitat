import { createHash, randomBytes } from "node:crypto";

/**
 * Pear's MCP OAuth endpoints accept dynamic client registration, which makes
 * them the one way to drive a full member sign-in (handle -> password ->
 * consent -> redirect with a code) without any external client or PDS.
 */
export interface McpClient {
  clientId: string;
  redirectUri: string;
  /** Builds the /authorize URL a browser is sent to, with a fresh PKCE challenge. */
  authorizeUrl(): { url: string; codeVerifier: string; state: string };
}

export async function registerMcpClient(
  api: { baseUrl: string },
  browserOrigin: string,
  redirectUri = "http://localhost:9999/callback",
  clientName = "E2E client",
): Promise<McpClient> {
  const res = await fetch(`${api.baseUrl}/mcp/oauth/register`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      client_name: clientName,
      redirect_uris: [redirectUri],
      grant_types: ["authorization_code"],
      response_types: ["code"],
      token_endpoint_auth_method: "none",
    }),
  });
  if (!res.ok)
    throw new Error(`register client ${res.status}: ${await res.text()}`);
  const { client_id: clientId } = (await res.json()) as { client_id: string };
  return {
    clientId,
    redirectUri,
    authorizeUrl() {
      const codeVerifier = randomBytes(32).toString("base64url");
      const state = randomBytes(8).toString("hex");
      const params = new URLSearchParams({
        response_type: "code",
        client_id: clientId,
        redirect_uri: redirectUri,
        state,
        code_challenge: createHash("sha256")
          .update(codeVerifier)
          .digest("base64url"),
        code_challenge_method: "S256",
        scope: "mcp",
      });
      return {
        url: `${browserOrigin}/mcp/oauth/authorize?${params}`,
        codeVerifier,
        state,
      };
    },
  };
}
