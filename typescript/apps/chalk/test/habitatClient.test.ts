import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";
import {
  EmailDomainNotFoundError,
  HabitatClient,
  startLogin,
} from "../src/server/habitatClient";
import { authorize } from "./fakeOAuth";

vi.mock("../src/server/oauth", () => import("./fakeOAuth"));

const testEnv = {} as Env;

describe("startLogin", () => {
  beforeEach(() => authorize.mockClear());

  it("authorizes the login hint as a member sign-in, returning the authorization URL", async () => {
    const url = await startLogin(testEnv, "alice.test");
    expect(url).toBe("https://pear.test/oauth/authorize");
    expect(authorize).toHaveBeenCalledWith("alice.test", {
      scope: "atproto",
      state: JSON.stringify({ kind: "member" }),
    });
  });

  it("carries an org connect's state through the round-trip", async () => {
    const state = {
      kind: "org" as const,
      orgDid: "did:web:org.example",
      memberDid: "did:plc:member1",
    };
    await startLogin(testEnv, "did:web:org.example", state);
    expect(authorize).toHaveBeenCalledWith("did:web:org.example", {
      scope: "atproto",
      state: JSON.stringify(state),
    });
  });

  it("throws EmailDomainNotFoundError when a work email's domain isn't mapped", async () => {
    // The OAuth client wraps the identity resolver's error, so DidNotFound
    // arrives as a cause.
    const resolverError = Object.assign(new Error("not found"), {
      xrpcError: "DidNotFound",
    });
    authorize.mockRejectedValueOnce(
      new Error("resolve failed", { cause: resolverError }),
    );
    await expect(startLogin(testEnv, "bob@gmail.com")).rejects.toBeInstanceOf(
      EmailDomainNotFoundError,
    );
  });

  it("rethrows other failures as they are", async () => {
    const boom = new Error("boom");
    authorize.mockRejectedValueOnce(boom);
    await expect(startLogin(testEnv, "alice.test")).rejects.toBe(boom);
  });
});

describe("HabitatClient", () => {
  const server = setupServer();
  beforeEach(() => server.listen({ onUnhandledRequest: "error" }));
  afterEach(() => {
    server.resetHandlers();
    server.close();
  });

  it("calls through the DID's own OAuth session, with GET params in the query", async () => {
    let request: Request | undefined;
    server.use(
      http.get(
        "http://pds.test/xrpc/network.habitat.space.listRecords",
        ({ request: r }) => {
          request = r;
          return HttpResponse.json({ records: [] });
        },
      ),
    );
    const client = new HabitatClient(testEnv, "did:plc:member1");
    await client.call("network.habitat.space.listRecords", "GET", {
      space: "at://did:plc:owner/space/network.habitat.docs/abc",
      cursor: undefined,
    });
    expect(request?.headers.get("X-Test-Did")).toBe("did:plc:member1");
    expect(new URL(request!.url).searchParams.toString()).toBe(
      new URLSearchParams({
        space: "at://did:plc:owner/space/network.habitat.docs/abc",
      }).toString(),
    );
  });

  it("asDid calls through the other DID's session", async () => {
    let did: string | null = null;
    server.use(
      http.post(
        "http://pds.test/xrpc/network.habitat.relationship.setSpaceRelation",
        ({ request }) => {
          did = request.headers.get("X-Test-Did");
          return HttpResponse.json({ uri: "at://x" });
        },
      ),
    );
    await new HabitatClient(testEnv, "did:plc:member1")
      .asDid("did:plc:owner")
      .call("network.habitat.relationship.setSpaceRelation", "POST", {});
    expect(did).toBe("did:plc:owner");
  });

  it("sends a POST's payload as JSON, with Atproto-Proxy when asked", async () => {
    let body: unknown;
    let proxy: string | null = null;
    server.use(
      http.post(
        "http://pds.test/xrpc/community.opensocial.createSpace",
        async ({ request }) => {
          body = await request.json();
          proxy = request.headers.get("Atproto-Proxy");
          return HttpResponse.json({ uri: "at://x" });
        },
      ),
    );
    const out = await new HabitatClient(testEnv, "did:plc:member1").call<{
      uri: string;
    }>(
      "community.opensocial.createSpace",
      "POST",
      { org: "did:web:org.example" },
      { atprotoProxy: "did:web:org.example#habitat" },
    );
    expect(out).toEqual({ uri: "at://x" });
    expect(body).toEqual({ org: "did:web:org.example" });
    expect(proxy).toBe("did:web:org.example#habitat");
  });

  it("throws with the response body on a non-2xx response", async () => {
    server.use(
      http.post(
        "http://pds.test/xrpc/network.habitat.simplespace.createSpace",
        () =>
          HttpResponse.json({ error: "SpaceAlreadyExists" }, { status: 400 }),
      ),
    );
    await expect(
      new HabitatClient(testEnv, "did:plc:member1").call(
        "network.habitat.simplespace.createSpace",
        "POST",
        {},
      ),
    ).rejects.toThrow(
      'network.habitat.simplespace.createSpace failed (400): {"error":"SpaceAlreadyExists"}',
    );
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
        "http://pds.test/xrpc/network.habitat.relationship.deleteRelation",
        () => new HttpResponse(null, { status: 200 }),
      ),
    );
    const client = new HabitatClient(testEnv, "did:plc:member1");
    await expect(
      client.call("network.habitat.relationship.deleteRelation", "POST", {
        uri: "at://did:plc:owner/space/network.habitat.docs/abc/rel/xyz",
      }),
    ).resolves.toBeUndefined();
  });

  it("uploadBlob POSTs raw bytes to network.habitat.repo.uploadBlob and returns the blob ref", async () => {
    server.use(
      http.post(
        "http://pds.test/xrpc/network.habitat.repo.uploadBlob",
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
    const client = new HabitatClient(testEnv, "did:plc:member1");
    const out = await client.uploadBlob(
      new Uint8Array([1, 2, 3]),
      "application/octet-stream",
    );
    expect(out.cid).toBe("bafy123");
  });
});
