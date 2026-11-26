import {
  afterAll,
  afterEach,
  beforeAll,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import { warmHabitat } from "./warmHabitat";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import type { DidString } from "@atproto/syntax";
import { getSpaceBlob } from "../src/server/blobs";
import { TEST_DID_HEADER } from "./fakeOAuth";

vi.mock("../src/server/oauth", () => import("./fakeOAuth"));

beforeAll(warmHabitat, 60_000);

const ORG = "did:web:org.example" as DidString;
const MEMBER = "did:web:alice.example" as DidString;
const SPACE = `at:space/${ORG}/network.habitat.docs/abc`;

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("getSpaceBlob", () => {
  it("reads through network.habitat.space.getBlob as the space's group, with no credential exchange", async () => {
    const calls: { path: string; as: string | null }[] = [];
    server.use(
      http.all("http://pds.test/xrpc/*", ({ request }) => {
        const url = new URL(request.url);
        calls.push({
          path: url.pathname,
          as: request.headers.get(TEST_DID_HEADER),
        });
        if (url.pathname !== "/xrpc/network.habitat.space.getBlob") {
          return new HttpResponse(null, { status: 500 });
        }
        expect(url.searchParams.get("space")).toBe(SPACE);
        expect(url.searchParams.get("repo")).toBe(MEMBER);
        expect(url.searchParams.get("cid")).toBe("bafkblob");
        return new HttpResponse(new Uint8Array([1, 2, 3]));
      }),
    );

    // MEMBER wrote the blob, but the group reads it.
    const bytes = await getSpaceBlob({} as Env, SPACE, MEMBER, "bafkblob");

    expect(bytes).toEqual(new Uint8Array([1, 2, 3]));
    expect(calls).toEqual([
      { path: "/xrpc/network.habitat.space.getBlob", as: ORG },
    ]);
  });

  it("fails when pear doesn't have the blob", async () => {
    server.use(
      http.get("http://pds.test/xrpc/network.habitat.space.getBlob", () =>
        HttpResponse.json({ error: "BlobNotFound" }, { status: 404 }),
      ),
    );
    await expect(
      getSpaceBlob({} as Env, SPACE, MEMBER, "bafkgone"),
    ).rejects.toThrow(/404/);
  });
});
