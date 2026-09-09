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

describe("docRole", () => {
  const server = setupServer();
  beforeEach(() => server.listen({ onUnhandledRequest: "error" }));
  afterEach(() => {
    server.resetHandlers();
    server.close();
  });

  const DOC = "at://did:web:alice.example/space/network.habitat.docs/abc";
  const COMMENTS =
    "at://did:web:alice.example/space/network.habitat.docs.comments/abc";
  const BOB = "did:plc:bob";

  // allow lists the (space, relation) pairs pear should say yes to; every
  // other check answers false. The two spaces inherit from each other, so
  // a realistic fixture has to include the relations that inheritance
  // implies, not just the one that was granted directly.
  function grant(allow: [string, string][]) {
    server.use(
      http.get(
        "http://sap-internal.test/proxy/network.habitat.relationship.checkUserRelation",
        ({ request }) => {
          const params = new URL(request.url).searchParams;
          const allowed = allow.some(
            ([space, relation]) =>
              params.get("space") === space &&
              params.get("relation") === relation,
          );
          return HttpResponse.json({ allowed });
        },
      ),
    );
  }

  async function roleOf(allow: [string, string][]) {
    grant(allow);
    const { SapClient } = await import("../src/server/sapClient");
    const { docRole } = await import("../src/server/functions.server");
    return docRole(new SapClient(testEnv, BOB), BOB, DOC);
  }

  it("is editor for a writer on the doc space", async () => {
    // An editor is a comments-space writer too, through the inheritance —
    // the doc-space check has to win over it.
    expect(
      await roleOf([
        [DOC, "writer"],
        [DOC, "reader"],
        [COMMENTS, "writer"],
      ]),
    ).toBe("editor");
  });

  it("is commenter for a writer on the comments space only", async () => {
    // A commenter reads the doc through the comments space's inheritance,
    // so the doc-space reader check passes for them too — commenter has to
    // win over viewer.
    expect(
      await roleOf([
        [COMMENTS, "writer"],
        [DOC, "reader"],
      ]),
    ).toBe("commenter");
  });

  it("is viewer for a reader on the doc space", async () => {
    expect(await roleOf([[DOC, "reader"]])).toBe("viewer");
  });

  it("is null with no relation at all", async () => {
    expect(await roleOf([])).toBeNull();
  });

  it("is null for a malformed docId (no comments space to check)", async () => {
    grant([]);
    const { SapClient } = await import("../src/server/sapClient");
    const { docRole } = await import("../src/server/functions.server");
    expect(await docRole(new SapClient(testEnv, BOB), BOB, "not-a-uri")).toBe(
      null,
    );
  });
});

describe("deleteUserGrant", () => {
  const server = setupServer();
  beforeEach(() => server.listen({ onUnhandledRequest: "error" }));
  afterEach(() => {
    server.resetHandlers();
    server.close();
  });

  const SPACE = "at://did:web:alice.example/space/network.habitat.docs/abc";
  const GRANT = `${SPACE}/did:web:alice.example/network.habitat.relationship.userRelation/rk1`;
  const BOB = "did:plc:bob";

  function relations(found: { uri: string }[]) {
    const deleted: unknown[] = [];
    server.use(
      http.get(
        "http://sap-internal.test/proxy/network.habitat.relationship.listRelations",
        ({ request }) => {
          const params = new URL(request.url).searchParams;
          expect(params.get("space")).toBe(SPACE);
          expect(params.get("subjectDid")).toBe(BOB);
          expect(params.get("subjectType")).toBe("user");
          return HttpResponse.json({ relations: found });
        },
      ),
      http.post(
        "http://sap-internal.test/proxy/network.habitat.relationship.deleteRelation",
        async ({ request }) => {
          deleted.push(await request.json());
          return new HttpResponse(null, { status: 200 });
        },
      ),
    );
    return deleted;
  }

  it("deletes the subject's grant on the space and returns its record uri", async () => {
    const deleted = relations([
      { uri: GRANT, subject: BOB, relation: "writer" },
    ]);
    const { SapClient } = await import("../src/server/sapClient");
    const { deleteUserGrant } = await import("../src/server/functions.server");
    const client = new SapClient(testEnv, BOB);
    expect(await deleteUserGrant(client, SPACE, BOB)).toBe(GRANT);
    expect(deleted).toEqual([{ uri: GRANT }]);
  });

  it("is a no-op when the subject holds no grant on the space", async () => {
    const deleted = relations([]);
    const { SapClient } = await import("../src/server/sapClient");
    const { deleteUserGrant } = await import("../src/server/functions.server");
    const client = new SapClient(testEnv, BOB);
    expect(await deleteUserGrant(client, SPACE, BOB)).toBeUndefined();
    expect(deleted).toEqual([]);
  });
});
