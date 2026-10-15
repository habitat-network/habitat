// Client side of the RFC 7523 JWT bearer grant against a Habitat instance's
// authorization server (pear's oauthserver, see HandleToken). A server-side
// app that lost an org's OAuth session — or whose refresh token expired —
// uses it to mint a fresh session without sending an admin through the
// browser flow again: it signs an assertion (iss = its OAuth client_id,
// sub = the org's DID, aud = the server's issuer) with a key published in
// its client metadata `jwks`, and pear checks that against an allow-list of
// approved clients (the --builtin_app flag).
//
// Written against WebCrypto only so it runs in Cloudflare Workers; building
// the @atproto/oauth-client Session out of the result is left to the app,
// which already depends on a JWK implementation for that.

export const JWT_BEARER_GRANT_TYPE =
  "urn:ietf:params:oauth:grant-type:jwt-bearer";

// A private ES256 JWK with the `kid` the app's client metadata publishes the
// matching public key under.
export type AssertionKey = JsonWebKey & { kid: string };

export interface JwtBearerParams {
  // Base URL of the Habitat instance, e.g. https://pear.habitat.network.
  habitatUrl: string;
  // The app's OAuth client_id (its client metadata URL).
  clientId: string;
  assertionKey: AssertionKey;
  // DID the token is minted for — the org whose session was lost.
  subject: string;
  scope: string;
  fetch?: typeof fetch;
}

export interface JwtBearerToken {
  issuer: string;
  sub: string;
  scope: string;
  accessToken: string;
  refreshToken?: string;
  // ISO date, when the server reported a lifetime.
  expiresAt?: string;
  // Private JWK of the DPoP key the access token is bound to.
  dpopJwk: JsonWebKey;
}

// Raised when the server rejects the grant, e.g. the app isn't on pear's
// allow-list or its published jwks don't carry the assertion key.
export class JwtBearerError extends Error {}

function b64url(bytes: ArrayBuffer | Uint8Array): string {
  let s = "";
  for (const b of new Uint8Array(bytes)) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function b64urlJson(value: unknown): string {
  return b64url(new TextEncoder().encode(JSON.stringify(value)));
}

async function signEs256(
  key: CryptoKey,
  header: Record<string, unknown>,
  payload: Record<string, unknown>,
): Promise<string> {
  const input = `${b64urlJson(header)}.${b64urlJson(payload)}`;
  // WebCrypto's ECDSA signature is the raw r||s concatenation JOSE wants.
  const sig = await crypto.subtle.sign(
    { name: "ECDSA", hash: "SHA-256" },
    key,
    new TextEncoder().encode(input),
  );
  return `${input}.${b64url(sig)}`;
}

const ES256 = { name: "ECDSA", namedCurve: "P-256" } as const;

// publicJwk strips the private part off an assertion key, giving the entry
// for client metadata's `jwks`.
export function publicJwk(key: AssertionKey): JsonWebKey & { kid: string } {
  const { kty, crv, x, y, kid } = key;
  return { kty, crv, x, y, kid, alg: "ES256", use: "sig" };
}

// parseAssertionKey parses the JSON an app keeps its assertion key in.
export function parseAssertionKey(json: string): AssertionKey {
  const key = JSON.parse(json) as AssertionKey;
  if (key.kty !== "EC" || key.crv !== "P-256" || !key.d || !key.kid) {
    throw new Error("assertion key must be a P-256 private JWK with a kid");
  }
  return key;
}

export async function requestJwtBearerToken(
  p: JwtBearerParams,
): Promise<JwtBearerToken> {
  const doFetch = p.fetch ?? fetch;
  const base = p.habitatUrl.replace(/\/+$/, "");

  const metaRes = await doFetch(
    `${base}/.well-known/oauth-authorization-server`,
  );
  if (!metaRes.ok) {
    throw new JwtBearerError(
      `fetching authorization server metadata failed (${metaRes.status})`,
    );
  }
  const meta = (await metaRes.json()) as {
    issuer: string;
    token_endpoint: string;
  };

  const now = Math.floor(Date.now() / 1000);
  const signingKey = await crypto.subtle.importKey(
    "jwk",
    p.assertionKey,
    ES256,
    false,
    ["sign"],
  );
  const assertion = await signEs256(
    signingKey,
    { alg: "ES256", typ: "JWT", kid: p.assertionKey.kid },
    {
      iss: p.clientId,
      sub: p.subject,
      aud: meta.issuer,
      iat: now,
      exp: now + 60,
      jti: crypto.randomUUID(),
    },
  );

  // atproto access tokens are DPoP-bound, so the session needs its own key;
  // the app keeps it with the tokens.
  const dpop = await crypto.subtle.generateKey(ES256, true, ["sign"]);
  // JoseKey (what @atproto/oauth-client keeps sessions' DPoP keys as) wants
  // a kid and alg on every key.
  const dpopJwk = {
    ...(await crypto.subtle.exportKey("jwk", dpop.privateKey)),
    kid: crypto.randomUUID(),
    alg: "ES256",
    use: "sig",
  };
  const dpopPublic = {
    kty: dpopJwk.kty,
    crv: dpopJwk.crv,
    x: dpopJwk.x,
    y: dpopJwk.y,
  };
  const body = new URLSearchParams({
    grant_type: JWT_BEARER_GRANT_TYPE,
    assertion,
    scope: p.scope,
  }).toString();

  let nonce: string | undefined;
  for (let attempt = 0; ; attempt++) {
    const proof = await signEs256(
      dpop.privateKey,
      { typ: "dpop+jwt", alg: "ES256", jwk: dpopPublic },
      {
        jti: crypto.randomUUID(),
        htm: "POST",
        htu: meta.token_endpoint,
        iat: Math.floor(Date.now() / 1000),
        ...(nonce ? { nonce } : {}),
      },
    );
    const res = await doFetch(meta.token_endpoint, {
      method: "POST",
      headers: {
        "content-type": "application/x-www-form-urlencoded",
        dpop: proof,
      },
      body,
    });
    nonce = res.headers.get("dpop-nonce") ?? nonce;
    if (res.ok) {
      const tok = (await res.json()) as {
        access_token: string;
        refresh_token?: string;
        expires_in?: number;
        scope?: string;
        sub?: string;
      };
      return {
        issuer: meta.issuer,
        sub: tok.sub ?? p.subject,
        scope: tok.scope ?? p.scope,
        accessToken: tok.access_token,
        refreshToken: tok.refresh_token,
        expiresAt:
          tok.expires_in === undefined
            ? undefined
            : new Date(Date.now() + tok.expires_in * 1000).toISOString(),
        dpopJwk,
      };
    }
    const text = await res.text();
    // The server may demand a DPoP nonce before it accepts a proof.
    if (
      attempt === 0 &&
      res.status === 400 &&
      text.includes("use_dpop_nonce")
    ) {
      continue;
    }
    throw new JwtBearerError(
      `jwt bearer token request failed (${res.status}): ${text}`,
    );
  }
}
