import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";
import { SapClient, startLogin } from "../src/server/sapClient";

const server = setupServer();
beforeEach(() => {
  process.env.CHALK_SAP_INTERNAL_URL = "http://sap-internal.test";
  process.env.CHALK_BASE_URL = "https://chalk.test";
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
      http.post("http://sap-internal.test/org/add", async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({
          redirect_url: "https://pds.example/authorize",
        });
      }),
    );
    const url = await startLogin("alice.test");
    expect(url).toBe("https://pds.example/authorize");
    expect(body).toEqual({
      handle: "alice.test",
      return_to: "https://chalk.test/session/callback",
    });
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
    const client = new SapClient("did:plc:member1");
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
    const client = new SapClient("did:plc:member1");
    const out = await client.uploadBlob(
      new Uint8Array([1, 2, 3]),
      "application/octet-stream",
    );
    expect(out.cid).toBe("bafy123");
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
    const client = new SapClient("did:plc:member1");
    await client.trackSpace(
      "at://did:plc:owner/space/network.habitat.docs/abc",
    );
    expect(body).toEqual({
      space: "at://did:plc:owner/space/network.habitat.docs/abc",
    });
    expect(headers?.get("Habitat-Did")).toBe("did:plc:member1");
  });
});
