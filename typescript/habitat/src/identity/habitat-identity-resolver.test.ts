import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "../test/msw.js";
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
