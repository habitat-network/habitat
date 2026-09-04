import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";
import { SapClient, startLogin } from "../src/server/sapClient";

const testEnv = {
  CHALK_SAP_INTERNAL_URL: "http://sap-internal.test",
  CHALK_BASE_URL: "https://chalk.test",
} as Env;

const server = setupServer();
beforeEach(() => {
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => {
  server.resetHandlers();
  server.close();
});

describe("startLogin", () => {
  it("posts handle and return_to, returns the redirect URL", async () => {
    let body: unknown;
    server.use(
      http.post("http://sap-internal.test/session/add", async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({
          redirect_url: "https://pds.example/authorize",
        });
      }),
    );
    const url = await startLogin(testEnv, "alice.test");
    expect(url).toBe("https://pds.example/authorize");
    expect(body).toEqual({
      handle: "alice.test",
      return_to: "https://chalk.test/session/callback",
    });
  });

  it("posts a custom return_to path when given one", async () => {
    let body: unknown;
    server.use(
      http.post("http://sap-internal.test/session/add", async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({
          redirect_url: "https://pear.example/oauth/authorize",
        });
      }),
    );
    await startLogin(testEnv, "did:web:org.example", "/session/org-callback");
    expect(body).toEqual({
      handle: "did:web:org.example",
      return_to: "https://chalk.test/session/org-callback",
    });
  });
});

describe("internal auth", () => {
  // sap's internal routes sit behind HTTP basic auth when it runs with
  // --internal-auth-secret (cmd/sap/server.go's basicAuthMiddleware); the
  // username is ignored, so it stays empty.
  const secretEnv = {
    ...testEnv,
    CHALK_SAP_INTERNAL_AUTH_SECRET: "s3cret",
  } as Env;

  async function captureHeaders(
    env: Env,
    fn: (client: SapClient) => Promise<unknown>,
  ): Promise<Headers> {
    let headers: Headers | undefined;
    server.use(
      http.get(
        "http://sap-internal.test/proxy/network.habitat.space.listRecords",
        ({ request }) => {
          headers = request.headers;
          return HttpResponse.json({ records: [] });
        },
      ),
    );
    await fn(new SapClient(env, "did:plc:member1"));
    expect(headers).toBeDefined();
    return headers!;
  }

  it("sends basic auth on proxied calls when the secret is set", async () => {
    const headers = await captureHeaders(secretEnv, (client) =>
      client.call("network.habitat.space.listRecords", "GET", {}),
    );
    expect(headers.get("Authorization")).toBe(`Basic ${btoa(":s3cret")}`);
  });

  it("sends basic auth on session/add when the secret is set", async () => {
    let headers: Headers | undefined;
    server.use(
      http.post("http://sap-internal.test/session/add", ({ request }) => {
        headers = request.headers;
        return HttpResponse.json({
          redirect_url: "https://pds.example/authorize",
        });
      }),
    );
    await startLogin(secretEnv, "alice.test");
    expect(headers?.get("Authorization")).toBe(`Basic ${btoa(":s3cret")}`);
  });

  it("sends no Authorization header without a secret (local dev)", async () => {
    const headers = await captureHeaders(testEnv, (client) =>
      client.call("network.habitat.space.listRecords", "GET", {}),
    );
    expect(headers.get("Authorization")).toBeNull();
  });
});

describe("SapClient", () => {
  it("sends the Habitat-Did header on every call", async () => {
    let headers: Headers | undefined;
    server.use(
      http.get(
        "http://sap-internal.test/proxy/network.habitat.space.listRecords",
        ({ request }) => {
          headers = request.headers;
          return HttpResponse.json({ records: [] });
        },
      ),
    );
    const client = new SapClient(testEnv, "did:plc:member1");
    await client.call("network.habitat.space.listRecords", "GET", {});
    expect(headers?.get("Habitat-Did")).toBe("did:plc:member1");
  });

  it("uploadBlob POSTs raw bytes to network.habitat.repo.uploadBlob and returns the blob ref", async () => {
    server.use(
      http.post(
        "http://sap-internal.test/proxy/network.habitat.repo.uploadBlob",
        async ({ request }) => {
          const buf = await request.arrayBuffer();
          expect(new Uint8Array(buf)).toEqual(new Uint8Array([1, 2, 3]));
          expect(request.headers.get("content-type")).toBe(
            "application/octet-stream",
          );
          return HttpResponse.json({
            blob: { ref: "bafy123" },
            cid: "bafy123",
          });
        },
      ),
    );
    const client = new SapClient(testEnv, "did:plc:member1");
    const out = await client.uploadBlob(
      new Uint8Array([1, 2, 3]),
      "application/octet-stream",
    );
    expect(out.cid).toBe("bafy123");
  });

  // Regression test: a procedure with no output schema (e.g.
  // network.habitat.relationship.deleteRelation) returns an empty 200 body.
  // call() used to do an unconditional res.json(), which throws "Unexpected
  // end of JSON input" on an empty body — surfaced live as "Couldn't remove
  // access: Unexpected end of JSON input" when deleteRelation's caller
  // ignores the return value, exactly as it's meant to.
  it("does not throw on a 200 response with an empty body", async () => {
    server.use(
      http.post(
        "http://sap-internal.test/proxy/network.habitat.relationship.deleteRelation",
        () => new HttpResponse(null, { status: 200 }),
      ),
    );
    const client = new SapClient(testEnv, "did:plc:member1");
    await expect(
      client.call("network.habitat.relationship.deleteRelation", "POST", {
        uri: "at://did:plc:owner/space/network.habitat.docs/abc/rel/xyz",
      }),
    ).resolves.toBeUndefined();
  });

  it("trackSpace POSTs the space URI with the auth header to /space/track", async () => {
    let body: unknown;
    let headers: Headers | undefined;
    server.use(
      http.post("http://sap-internal.test/space/track", async ({ request }) => {
        body = await request.json();
        headers = request.headers;
        return new HttpResponse(null, { status: 200 });
      }),
    );
    const client = new SapClient(testEnv, "did:plc:member1");
    await client.trackSpace(
      "at://did:plc:owner/space/network.habitat.docs/abc",
    );
    expect(body).toEqual({
      space: "at://did:plc:owner/space/network.habitat.docs/abc",
    });
    expect(headers?.get("Habitat-Did")).toBe("did:plc:member1");
  });

  it("sets Atproto-Proxy when given an atprotoProxy target", async () => {
    let headers: Headers | undefined;
    server.use(
      http.post(
        "http://sap-internal.test/proxy/community.opensocial.createSpace",
        ({ request }) => {
          headers = request.headers;
          return HttpResponse.json({ uri: "at://did:web:org.example/x" });
        },
      ),
    );
    const client = new SapClient(testEnv, "did:plc:member1");
    await client.call(
      "community.opensocial.createSpace",
      "POST",
      { org: "did:web:org.example", type: "network.habitat.docs" },
      { atprotoProxy: "did:web:org.example#habitat" },
    );
    expect(headers?.get("Atproto-Proxy")).toBe("did:web:org.example#habitat");
  });

  it("omits Atproto-Proxy when no atprotoProxy is given", async () => {
    let headers: Headers | undefined;
    server.use(
      http.get(
        "http://sap-internal.test/proxy/network.habitat.space.listRecords",
        ({ request }) => {
          headers = request.headers;
          return HttpResponse.json({ records: [] });
        },
      ),
    );
    const client = new SapClient(testEnv, "did:plc:member1");
    await client.call("network.habitat.space.listRecords", "GET", {});
    expect(headers?.has("Atproto-Proxy")).toBe(false);
  });

  it("recrawl POSTs to /session/recrawl with the Habitat-Did header", async () => {
    let headers: Headers | undefined;
    server.use(
      http.post("http://sap-internal.test/session/recrawl", ({ request }) => {
        headers = request.headers;
        return new HttpResponse(null, { status: 202 });
      }),
    );
    const client = new SapClient(testEnv, "did:plc:member1");
    await client.recrawl();
    expect(headers?.get("Habitat-Did")).toBe("did:plc:member1");
  });
});
