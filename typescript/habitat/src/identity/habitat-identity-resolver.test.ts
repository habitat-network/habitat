import { createServer, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";

import { delay, http, HttpResponse, passthrough } from "msw";
import { afterEach, describe, expect, it } from "vitest";

import { server } from "../test/msw.js";
import { HabitatIdentityResolverError } from "./errors.js";
import {
  DEFAULT_HABITAT_SERVICE_URL,
  HabitatIdentityResolver,
} from "./habitat-identity-resolver.js";

const XRPC_PATH = "/xrpc/com.atproto.identity.resolveIdentity";
const DID = "did:web:acme.habitat.network";
const HANDLE = "acme.habitat.network";

const DID_DOC = {
  "@context": ["https://www.w3.org/ns/did/v1"],
  id: DID,
  alsoKnownAs: [`at://${HANDLE}`],
  verificationMethod: [],
  service: [
    {
      id: "#atproto_pds",
      type: "AtprotoPersonalDataServer",
      serviceEndpoint: "https://pds.example.com",
    },
  ],
};

/**
 * Registers a success handler on `origin` and returns a getter for the
 * `identifier` query param the resolver actually sent.
 */
function stubResolveIdentity(
  origin: string,
  body: unknown = { did: DID, handle: HANDLE, didDoc: DID_DOC },
): () => string | null {
  let received: string | null = null;

  server.use(
    http.get(`${origin}${XRPC_PATH}`, ({ request }) => {
      received = new URL(request.url).searchParams.get("identifier");
      return HttpResponse.json(body);
    }),
  );

  return () => received;
}

/** Local HTTP servers started by `withStalledErrorBody`, torn down per test. */
const stalledServers: Array<() => Promise<void>> = [];

afterEach(async () => {
  while (stalledServers.length > 0) await stalledServers.pop()!();
});

/**
 * Starts a real local HTTP server whose handler is expected to flush response
 * headers and then never end the body, and returns its origin. Needed because a
 * mocked MSW body cannot stall *after* headers reach `fetch`.
 */
async function withStalledErrorBody(
  respond: (res: ServerResponse) => void,
): Promise<string> {
  const httpServer = createServer((_req, res) => respond(res));

  await new Promise<void>((resolve) => {
    httpServer.listen(0, "127.0.0.1", resolve);
  });

  stalledServers.push(
    () =>
      new Promise<void>((resolve) => {
        httpServer.closeAllConnections();
        httpServer.close(() => resolve());
      }),
  );

  const { port } = httpServer.address() as AddressInfo;
  return `http://127.0.0.1:${port}`;
}

describe("HabitatIdentityResolver", () => {
  it("resolves a did:web Habitat identity", async () => {
    const identifier = stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL);

    const info = await new HabitatIdentityResolver().resolve(DID);

    expect(identifier()).toBe(DID);
    expect(info).toEqual({ did: DID, handle: HANDLE, didDoc: DID_DOC });
  });

  it("resolves a handle", async () => {
    const identifier = stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL);

    const info = await new HabitatIdentityResolver().resolve(HANDLE);

    expect(identifier()).toBe(HANDLE);
    expect(info.did).toBe(DID);
  });

  it("calls a custom service URL and never the default", async () => {
    // The default origin is intentionally left unregistered: MSW's
    // onUnhandledRequest: "error" turns a regression here into a failure.
    const identifier = stubResolveIdentity("https://pear.example.internal");

    const info = await new HabitatIdentityResolver(
      "https://pear.example.internal",
    ).resolve(HANDLE);

    expect(identifier()).toBe(HANDLE);
    expect(info.did).toBe(DID);
  });

  it("accepts a URL instance", async () => {
    stubResolveIdentity("https://pear.example.internal");

    const info = await new HabitatIdentityResolver(
      new URL("https://pear.example.internal"),
    ).resolve(HANDLE);

    expect(info.did).toBe(DID);
  });

  it("resolves the XRPC path against the origin, discarding any base path", async () => {
    stubResolveIdentity("https://pear.example.internal");

    const info = await new HabitatIdentityResolver(
      "https://pear.example.internal/some/base/path",
    ).resolve(HANDLE);

    expect(info.did).toBe(DID);
  });

  it("round-trips identifiers needing URL encoding", async () => {
    const identifier = stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL);

    await new HabitatIdentityResolver().resolve(
      "did:web:acme.habitat.network:sub:path",
    );

    expect(identifier()).toBe("did:web:acme.habitat.network:sub:path");
  });

  it("normalizes the returned handle", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      did: DID,
      handle: "ACME.Habitat.Network",
      didDoc: DID_DOC,
    });

    const info = await new HabitatIdentityResolver().resolve(DID);

    expect(info.handle).toBe("acme.habitat.network");
  });

  it("substitutes handle.invalid for a syntactically invalid handle", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      did: DID,
      handle: "not a handle",
      didDoc: DID_DOC,
    });

    const info = await new HabitatIdentityResolver().resolve(DID);

    expect(info.handle).toBe("handle.invalid");
  });

  it("passes handle.invalid through unchanged", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      did: DID,
      handle: "handle.invalid",
      didDoc: DID_DOC,
    });

    const info = await new HabitatIdentityResolver().resolve(DID);

    expect(info.handle).toBe("handle.invalid");
  });
});

describe("HabitatIdentityResolver error handling", () => {
  it("throws with status and xrpcError for a 404 DidNotFound", async () => {
    server.use(
      http.get(`${DEFAULT_HABITAT_SERVICE_URL}${XRPC_PATH}`, () =>
        HttpResponse.json(
          { error: "DidNotFound", message: "DID not found" },
          { status: 404 },
        ),
      ),
    );

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
    expect((error as HabitatIdentityResolverError).status).toBe(404);
    expect((error as HabitatIdentityResolverError).xrpcError).toBe(
      "DidNotFound",
    );
    expect((error as Error).message).toContain("DID not found");
  });

  it("throws with status and xrpcError for a 400 InvalidRequest", async () => {
    server.use(
      http.get(`${DEFAULT_HABITAT_SERVICE_URL}${XRPC_PATH}`, () =>
        HttpResponse.json(
          { error: "InvalidRequest", message: "invalid identifier" },
          { status: 400 },
        ),
      ),
    );

    const error = await new HabitatIdentityResolver()
      .resolve("!!!")
      .catch((e: unknown) => e);

    expect((error as HabitatIdentityResolverError).status).toBe(400);
    expect((error as HabitatIdentityResolverError).xrpcError).toBe(
      "InvalidRequest",
    );
  });

  it("still reports the status when the error body is not JSON", async () => {
    server.use(
      http.get(`${DEFAULT_HABITAT_SERVICE_URL}${XRPC_PATH}`, () =>
        HttpResponse.text("upstream exploded", { status: 502 }),
      ),
    );

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
    expect((error as HabitatIdentityResolverError).status).toBe(502);
    expect((error as HabitatIdentityResolverError).xrpcError).toBeUndefined();
  });

  it("throws when the success body is not JSON", async () => {
    server.use(
      http.get(`${DEFAULT_HABITAT_SERVICE_URL}${XRPC_PATH}`, () =>
        HttpResponse.text("not json", { status: 200 }),
      ),
    );

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
  });

  it("throws when the payload has no DID", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      handle: HANDLE,
      didDoc: DID_DOC,
    });

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
  });

  it("throws when the DID uses an unsupported method", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      did: "did:example:nope",
      handle: HANDLE,
      didDoc: { ...DID_DOC, id: "did:example:nope" },
    });

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
  });

  it("throws when the DID document is missing", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      did: DID,
      handle: HANDLE,
    });

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
  });

  it("throws when the DID document id does not match the resolved DID", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      did: DID,
      handle: HANDLE,
      didDoc: { ...DID_DOC, id: "did:web:someone-else.habitat.network" },
    });

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
    expect((error as Error).message).toContain("someone-else");
  });

  it("throws when the handle is not a string", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      did: DID,
      handle: 42,
      didDoc: DID_DOC,
    });

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
  });

  it("wraps a network failure and preserves the cause", async () => {
    server.use(
      http.get(`${DEFAULT_HABITAT_SERVICE_URL}${XRPC_PATH}`, () =>
        HttpResponse.error(),
      ),
    );

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
    expect((error as Error).cause).toBeDefined();
    expect((error as Error).message).toContain("pear.habitat.network");
  });

  it("requests an uncached response when noCache is set", async () => {
    let cacheMode: string | undefined;

    server.use(
      http.get(`${DEFAULT_HABITAT_SERVICE_URL}${XRPC_PATH}`, ({ request }) => {
        cacheMode = request.cache;
        return HttpResponse.json({ did: DID, handle: HANDLE, didDoc: DID_DOC });
      }),
    );

    const info = await new HabitatIdentityResolver().resolve(DID, {
      noCache: true,
    });

    expect(info.did).toBe(DID);
    // Known fragility: undici does not reliably surface `request.cache` to an
    // MSW handler. If `cacheMode` comes back as "default" or undefined, delete
    // this single assertion rather than adding a seam to production code to
    // make it observable — the behavior is a one-line pass-through to `fetch`.
    expect(cacheMode).toBe("no-store");
  });

  it("propagates AbortError without wrapping it", async () => {
    server.use(
      http.get(`${DEFAULT_HABITAT_SERVICE_URL}${XRPC_PATH}`, async () => {
        await delay("infinite");
        return HttpResponse.json({});
      }),
    );

    const controller = new AbortController();
    const promise = new HabitatIdentityResolver().resolve(DID, {
      signal: controller.signal,
    });
    controller.abort();

    const error = await promise.catch((e: unknown) => e);

    expect((error as Error).name).toBe("AbortError");
    expect(error).not.toBeInstanceOf(HabitatIdentityResolverError);
  });

  // MSW buffers a mocked response body before handing it to `fetch`, so a
  // stalled MSW body makes `fetch` itself never settle — the abort then lands
  // in the fetch catch and never reaches `toXrpcError`. Covering that third
  // catch site requires a real socket that flushes error headers and then
  // stalls the body, so this test passes through to a local HTTP server.
  it("propagates AbortError when a non-2xx body is aborted mid-read", async () => {
    let headersFlushed: () => void;
    const flushed = new Promise<void>((resolve) => {
      headersFlushed = resolve;
    });

    const origin = await withStalledErrorBody((res) => {
      res.writeHead(404, { "content-type": "application/json" });
      // Partial JSON: headers and a byte reach the client, then nothing more,
      // so `response.json()` is still awaiting data when the abort fires.
      res.write('{"error":', () => headersFlushed());
    });

    server.use(http.get(`${origin}${XRPC_PATH}`, () => passthrough()));

    const controller = new AbortController();
    const promise = new HabitatIdentityResolver(origin).resolve(DID, {
      signal: controller.signal,
    });

    await flushed;
    // Let the flushed headers resolve the `fetch` promise so the resolver is
    // inside the error-body read, not the fetch call, when the abort lands.
    await delay(50);
    controller.abort();

    const error = await promise.catch((e: unknown) => e);

    expect((error as Error).name).toBe("AbortError");
    expect(error).not.toBeInstanceOf(HabitatIdentityResolverError);
  });
});
