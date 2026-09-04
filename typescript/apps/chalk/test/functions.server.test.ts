import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";

const sessionData: { did?: string } = {};
vi.mock("../src/server/session", () => ({
  useAppSession: vi.fn(async () => ({
    data: sessionData,
    update: vi.fn(async (patch: Record<string, unknown>) =>
      Object.assign(sessionData, patch),
    ),
    clear: vi.fn(async () => {
      delete sessionData.did;
    }),
  })),
}));

const { requireSession } = await import("../src/server/functions.server");

describe("requireSession", () => {
  beforeEach(() => {
    delete sessionData.did;
  });

  it("returns the caller when a session DID is set", async () => {
    sessionData.did = "did:plc:member1";
    await expect(requireSession()).resolves.toEqual({
      did: "did:plc:member1",
      currentOrg: undefined,
    });
  });

  it("throws when no session DID is set", async () => {
    await expect(requireSession()).rejects.toThrow();
  });
});

const testEnv = {
  CHALK_SAP_INTERNAL_URL: "http://sap-internal.test",
} as Env;

describe("createDocSpace", () => {
  const server = setupServer();
  beforeEach(() => server.listen({ onUnhandledRequest: "error" }));
  afterEach(() => {
    server.resetHandlers();
    server.close();
  });

  it("creates a personal space via network.habitat.simplespace.createSpace", async () => {
    let body: unknown;
    let proxyHeader: string | null = null;
    server.use(
      http.post(
        "http://sap-internal.test/proxy/network.habitat.simplespace.createSpace",
        async ({ request }) => {
          body = await request.json();
          proxyHeader = request.headers.get("Atproto-Proxy");
          return HttpResponse.json({ uri: "at://did:plc:member1/space/x" });
        },
      ),
    );
    const { SapClient } = await import("../src/server/sapClient");
    const { createDocSpace } = await import("../src/server/functions.server");
    const client = new SapClient(testEnv, "did:plc:member1");
    const result = await createDocSpace(client, "did:plc:member1", undefined);
    expect(body).toEqual({
      did: "did:plc:member1",
      type: "network.habitat.docs",
    });
    expect(proxyHeader).toBeNull();
    expect(result).toEqual({
      uri: "at://did:plc:member1/space/x",
      ownerDid: "did:plc:member1",
      isOrg: false,
    });
  });

  it("creates an org space via community.opensocial.createSpace, proxied to the org", async () => {
    let body: unknown;
    let proxyHeader: string | null = null;
    server.use(
      http.post(
        "http://sap-internal.test/proxy/community.opensocial.createSpace",
        async ({ request }) => {
          body = await request.json();
          proxyHeader = request.headers.get("Atproto-Proxy");
          return HttpResponse.json({ uri: "at://did:web:org.example/space/x" });
        },
      ),
    );
    const { SapClient } = await import("../src/server/sapClient");
    const { createDocSpace } = await import("../src/server/functions.server");
    const client = new SapClient(testEnv, "did:plc:member1");
    const result = await createDocSpace(
      client,
      "did:plc:member1",
      "did:web:org.example",
    );
    expect(body).toEqual({
      org: "did:web:org.example",
      type: "network.habitat.docs",
      roles: ["admin", "member"],
    });
    expect(proxyHeader).toBe("did:web:org.example#habitat");
    expect(result).toEqual({
      uri: "at://did:web:org.example/space/x",
      ownerDid: "did:web:org.example",
      isOrg: true,
    });
  });
});

describe("fetchOrgName", () => {
  const server = setupServer();
  beforeEach(() => server.listen({ onUnhandledRequest: "error" }));
  afterEach(() => {
    server.resetHandlers();
    server.close();
  });

  it("reads the org's profile record via the member's own session (no proxy needed)", async () => {
    let params: URLSearchParams | undefined;
    let proxyHeader: string | null = null;
    server.use(
      http.get(
        "http://sap-internal.test/proxy/network.habitat.space.getRecord",
        ({ request }) => {
          params = new URL(request.url).searchParams;
          proxyHeader = request.headers.get("Atproto-Proxy");
          return HttpResponse.json({ value: { name: "Acme Corp" } });
        },
      ),
    );
    const { SapClient } = await import("../src/server/sapClient");
    const { fetchOrgName } = await import("../src/server/functions.server");
    const client = new SapClient(testEnv, "did:plc:member1");
    const name = await fetchOrgName(client, "did:web:org.example");
    expect(name).toBe("Acme Corp");
    // No Atproto-Proxy: the about space's own community.opensocial.access
    // record already admits any member/admin role, so pear's
    // CheckUserHasSpaceRole grants the read directly — see functions.server.ts.
    expect(proxyHeader).toBeNull();
    expect(params?.get("repo")).toBe("did:web:org.example");
    expect(params?.get("collection")).toBe("community.opensocial.profile");
    expect(params?.get("rkey")).toBe("self");
    expect(params?.get("space")).toBe(
      "at://did:web:org.example/space/community.opensocial.about/self",
    );
  });

  it("returns null when the read fails (not a member, org gone, etc.)", async () => {
    server.use(
      http.get(
        "http://sap-internal.test/proxy/network.habitat.space.getRecord",
        () => new HttpResponse("nope", { status: 400 }),
      ),
    );
    const { SapClient } = await import("../src/server/sapClient");
    const { fetchOrgName } = await import("../src/server/functions.server");
    const client = new SapClient(testEnv, "did:plc:member1");
    expect(await fetchOrgName(client, "did:web:org.example")).toBeNull();
  });
});

describe("listMyOrgIds", () => {
  const server = setupServer();
  beforeEach(() => server.listen({ onUnhandledRequest: "error" }));
  afterEach(() => {
    server.resetHandlers();
    server.close();
  });

  it("returns the owner DID of every community.opensocial.members space", async () => {
    server.use(
      http.get(
        "http://sap-internal.test/proxy/network.habitat.space.listSpaces",
        ({ request }) => {
          expect(new URL(request.url).searchParams.get("type")).toBe(
            "community.opensocial.members",
          );
          return HttpResponse.json({
            spaces: [
              {
                uri: "at://did:web:org1.example/space/community.opensocial.members/self",
              },
              {
                uri: "at://did:web:org2.example/space/community.opensocial.members/self",
              },
            ],
          });
        },
      ),
    );
    const { SapClient } = await import("../src/server/sapClient");
    const { listMyOrgIds } = await import("../src/server/functions.server");
    const client = new SapClient(testEnv, "did:plc:member1");
    expect(await listMyOrgIds(client)).toEqual([
      "did:web:org1.example",
      "did:web:org2.example",
    ]);
  });
});
