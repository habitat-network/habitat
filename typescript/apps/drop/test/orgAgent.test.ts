import { env as workerEnv } from "cloudflare:workers";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getDb } from "@/db";
import { oauthSessions } from "@/db/schema";
import { orgAgent } from "@/server/oauth";

const ORG = "did:web:org.example";
const HABITAT = "https://pear.test";

async function assertionKeyJson(): Promise<string> {
  const pair = await crypto.subtle.generateKey(
    { name: "ECDSA", namedCurve: "P-256" },
    true,
    ["sign", "verify"],
  );
  return JSON.stringify({
    ...(await crypto.subtle.exportKey("jwk", pair.privateKey)),
    kid: "k1",
  });
}

// fakePear stands in for the Habitat instance: its metadata, its token
// endpoint (counting JWT bearer grants, each issuing a numbered access
// token) and a resource endpoint that only accepts tokens in `valid`.
function fakePear() {
  const state = { grants: 0, valid: new Set<string>(), seen: [] as string[] };
  const fake = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(input instanceof Request ? input.url : String(input));
    if (url.pathname === "/.well-known/oauth-authorization-server") {
      return Response.json({
        issuer: HABITAT,
        token_endpoint: `${HABITAT}/oauth/token`,
        authorization_endpoint: `${HABITAT}/oauth/authorize`,
        pushed_authorization_request_endpoint: `${HABITAT}/oauth/par`,
        require_pushed_authorization_requests: true,
        response_types_supported: ["code"],
        grant_types_supported: [
          "authorization_code",
          "refresh_token",
          "urn:ietf:params:oauth:grant-type:jwt-bearer",
        ],
        code_challenge_methods_supported: ["S256"],
        token_endpoint_auth_methods_supported: ["none", "private_key_jwt"],
        token_endpoint_auth_signing_alg_values_supported: ["ES256"],
        scopes_supported: ["atproto"],
        dpop_signing_alg_values_supported: ["ES256"],
        authorization_response_iss_parameter_supported: true,
        client_id_metadata_document_supported: true,
      });
    }
    if (url.pathname === "/oauth/token") {
      const form = new URLSearchParams(String(init?.body));
      expect(form.get("grant_type")).toBe(
        "urn:ietf:params:oauth:grant-type:jwt-bearer",
      );
      const token = `at${++state.grants}`;
      state.valid.add(token);
      return Response.json({
        access_token: token,
        refresh_token: `rt${state.grants}`,
        expires_in: 3600,
        scope: form.get("scope"),
        sub: ORG,
        token_type: "DPoP",
      });
    }
    const auth =
      new Headers(input instanceof Request ? input.headers : init?.headers).get(
        "authorization",
      ) ?? "";
    state.seen.push(auth);
    const token = auth.replace(/^DPoP /, "");
    return state.valid.has(token)
      ? Response.json({ spaces: [] })
      : Response.json({ error: "InvalidToken" }, { status: 401 });
  };
  return { state, fake };
}

describe("orgAgent", () => {
  const db = getDb(workerEnv);
  const baseEnv = {
    ...workerEnv,
    DROP_BASE_URL: "https://drop.test",
    DROP_HABITAT_URL: HABITAT,
  } as Env;

  beforeEach(async () => {
    await db.delete(oauthSessions);
  });
  afterEach(() => vi.unstubAllGlobals());

  it("mints a session through the JWT bearer grant when none is stored", async () => {
    const pear = fakePear();
    vi.stubGlobal("fetch", pear.fake);
    const env = {
      ...baseEnv,
      DROP_ASSERTION_KEY: await assertionKeyJson(),
    } as Env;

    const agent = await orgAgent(env, ORG);
    const res = await agent.fetchHandler(
      "/xrpc/network.habitat.space.listSpaces",
      {
        method: "GET",
      },
    );

    expect(res.status).toBe(200);
    expect(pear.state.grants).toBe(1);
    expect(pear.state.seen).toEqual(["DPoP at1"]);
  });

  it("retries a 401 once on a freshly minted session", async () => {
    const pear = fakePear();
    vi.stubGlobal("fetch", pear.fake);
    const env = {
      ...baseEnv,
      DROP_ASSERTION_KEY: await assertionKeyJson(),
    } as Env;

    const agent = await orgAgent(env, ORG);
    // pear forgets the first token, as if the session were revoked.
    pear.state.valid.delete("at1");
    const res = await agent.fetchHandler(
      "/xrpc/network.habitat.space.listSpaces",
      {
        method: "GET",
      },
    );

    expect(res.status).toBe(200);
    expect(pear.state.grants).toBe(2);
    expect(pear.state.seen).toEqual(["DPoP at1", "DPoP at2"]);
  });

  it("surfaces the original failure when no assertion key is configured", async () => {
    const pear = fakePear();
    vi.stubGlobal("fetch", pear.fake);

    await expect(orgAgent(baseEnv, ORG)).rejects.toThrow();
    expect(pear.state.grants).toBe(0);
  });
});
