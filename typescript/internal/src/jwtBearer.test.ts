// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  type AssertionKey,
  JWT_BEARER_GRANT_TYPE,
  JwtBearerError,
  parseAssertionKey,
  publicJwk,
  requestJwtBearerToken,
} from "./jwtBearer";

const HABITAT = "https://pear.test";
const CLIENT_ID = "https://app.test/oauth/client-metadata.json";

async function newAssertionKey(): Promise<AssertionKey> {
  const pair = await crypto.subtle.generateKey(
    { name: "ECDSA", namedCurve: "P-256" },
    true,
    ["sign", "verify"],
  );
  return {
    ...(await crypto.subtle.exportKey("jwk", pair.privateKey)),
    kid: "k1",
  };
}

function decode(part: string) {
  return JSON.parse(atob(part.replace(/-/g, "+").replace(/_/g, "/")));
}

// verify checks a compact ES256 JWT against a public JWK and returns its
// header and payload.
async function verify(jwt: string, jwk: JsonWebKey) {
  const [h, p, s] = jwt.split(".");
  const key = await crypto.subtle.importKey(
    "jwk",
    jwk,
    { name: "ECDSA", namedCurve: "P-256" },
    false,
    ["verify"],
  );
  const sig = Uint8Array.from(
    atob(s.replace(/-/g, "+").replace(/_/g, "/")),
    (c) => c.charCodeAt(0),
  );
  const ok = await crypto.subtle.verify(
    { name: "ECDSA", hash: "SHA-256" },
    key,
    sig,
    new TextEncoder().encode(`${h}.${p}`),
  );
  return { ok, header: decode(h), payload: decode(p) };
}

// fakePear serves the two endpoints the grant touches. With requireNonce set
// it demands a DPoP nonce first, like a real server may.
function fakePear(
  assertionKey: AssertionKey,
  opts: { requireNonce?: boolean; reject?: boolean } = {},
) {
  const seen: {
    assertion?: Awaited<ReturnType<typeof verify>>;
    scope?: string;
  } = {};
  const f: typeof fetch = async (input, init) => {
    const url = String(input);
    if (url === `${HABITAT}/.well-known/oauth-authorization-server`) {
      return Response.json({
        issuer: HABITAT,
        token_endpoint: `${HABITAT}/oauth/token`,
      });
    }
    expect(url).toBe(`${HABITAT}/oauth/token`);
    const headers = new Headers(init?.headers);
    const proof = await verify(
      headers.get("dpop")!,
      decode(headers.get("dpop")!.split(".")[0]).jwk,
    );
    expect(proof.ok).toBe(true);
    expect(proof.header.typ).toBe("dpop+jwt");
    expect(proof.payload).toMatchObject({
      htm: "POST",
      htu: `${HABITAT}/oauth/token`,
    });
    if (opts.requireNonce && proof.payload.nonce !== "n1") {
      return new Response(JSON.stringify({ error: "use_dpop_nonce" }), {
        status: 400,
        headers: { "dpop-nonce": "n1" },
      });
    }
    if (opts.reject) {
      return new Response(JSON.stringify({ error: "invalid_grant" }), {
        status: 400,
      });
    }
    const form = new URLSearchParams(String(init?.body));
    expect(form.get("grant_type")).toBe(JWT_BEARER_GRANT_TYPE);
    seen.scope = form.get("scope") ?? undefined;
    seen.assertion = await verify(
      form.get("assertion")!,
      publicJwk(assertionKey),
    );
    return Response.json({
      access_token: "at",
      refresh_token: "rt",
      expires_in: 3600,
      scope: form.get("scope"),
      sub: "did:web:org.test",
    });
  };
  return { fetch: f, seen };
}

describe("requestJwtBearerToken", () => {
  it("signs an assertion for the org and returns the DPoP-bound token", async () => {
    const assertionKey = await newAssertionKey();
    const pear = fakePear(assertionKey);
    const tok = await requestJwtBearerToken({
      habitatUrl: `${HABITAT}/`,
      clientId: CLIENT_ID,
      assertionKey,
      subject: "did:web:org.test",
      scope: "atproto",
      fetch: pear.fetch,
    });

    expect(pear.seen.assertion?.ok).toBe(true);
    expect(pear.seen.assertion?.header).toMatchObject({
      alg: "ES256",
      kid: "k1",
    });
    expect(pear.seen.assertion?.payload).toMatchObject({
      iss: CLIENT_ID,
      sub: "did:web:org.test",
      aud: HABITAT,
    });
    expect(pear.seen.scope).toBe("atproto");
    expect(tok).toMatchObject({
      accessToken: "at",
      refreshToken: "rt",
      sub: "did:web:org.test",
      issuer: HABITAT,
    });
    expect(tok.expiresAt).toBeDefined();
    expect(tok.dpopJwk.d).toBeDefined();
  });

  it("retries once with the DPoP nonce the server asks for", async () => {
    const assertionKey = await newAssertionKey();
    const tok = await requestJwtBearerToken({
      habitatUrl: HABITAT,
      clientId: CLIENT_ID,
      assertionKey,
      subject: "did:web:org.test",
      scope: "atproto",
      fetch: fakePear(assertionKey, { requireNonce: true }).fetch,
    });
    expect(tok.accessToken).toBe("at");
  });

  it("throws JwtBearerError when the server rejects the grant", async () => {
    const assertionKey = await newAssertionKey();
    await expect(
      requestJwtBearerToken({
        habitatUrl: HABITAT,
        clientId: CLIENT_ID,
        assertionKey,
        subject: "did:web:org.test",
        scope: "atproto",
        fetch: fakePear(assertionKey, { reject: true }).fetch,
      }),
    ).rejects.toBeInstanceOf(JwtBearerError);
  });
});

describe("assertion keys", () => {
  it("publicJwk drops the private part", async () => {
    const pub = publicJwk(await newAssertionKey());
    expect(pub.d).toBeUndefined();
    expect(pub.kid).toBe("k1");
  });

  it("parseAssertionKey rejects a key without private material", async () => {
    const pub = publicJwk(await newAssertionKey());
    expect(() => parseAssertionKey(JSON.stringify(pub))).toThrow();
  });
});
