import { env } from "cloudflare:test";
import { beforeEach, expect, it, vi } from "vitest";
import type { SpaceRefString } from "@atproto/syntax";
import { getDb, upsertDoc, upsertDocAccess } from "../src/db";
import { issueDelegation } from "../src/server/sync";

vi.mock("../src/server/oauth", () => import("./fakeOAuth"));

const ORG = "did:web:org.example";
const DOC = `at://${ORG}/space/network.habitat.docs/abc` as SpaceRefString;
const COMMENTS =
  `at://${ORG}/space/network.habitat.docs.comments/abc` as SpaceRefString;
const BOB = "did:web:bob.example";

// fakeDelegation answers getDelegationToken as `allowed` only, recording
// which session each attempt was made with.
function fakeDelegation(allowed: string) {
  const tried: (string | null)[] = [];
  vi.stubGlobal(
    "fetch",
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const did = new Headers(init?.headers).get("X-Test-Did");
      tried.push(did);
      if (!String(input).includes("com.atproto.space.getDelegationToken")) {
        throw new Error(`unexpected fetch: ${input}`);
      }
      return did === allowed
        ? Response.json({ token: `token-for-${did}` })
        : Response.json({ error: "NoSession" }, { status: 401 });
    },
  );
  return tried;
}

beforeEach(async () => {
  await env.DB.exec("DELETE FROM docs");
  await env.DB.exec("DELETE FROM doc_access");
});

it("mints the delegation token as the space authority first", async () => {
  const tried = fakeDelegation(ORG);
  expect(await issueDelegation(env, DOC)).toBe(`token-for-${ORG}`);
  expect(tried).toEqual([ORG]);
});

it("falls back to members with a grant on the doc when the authority has no session", async () => {
  await upsertDoc(getDb(env), {
    spaceUri: DOC,
    docId: DOC,
    ownerDid: ORG,
    title: "Untitled",
  });
  await upsertDocAccess(getDb(env), {
    uri: `${DOC}/${ORG}/network.habitat.relationship.userRelation/r1`,
    spaceUri: DOC,
    subjectDid: BOB,
    relation: "reader",
  });
  const tried = fakeDelegation(BOB);
  // A comments space's grants are filed under its doc (see sink.ts).
  expect(await issueDelegation(env, COMMENTS)).toBe(`token-for-${BOB}`);
  expect(tried).toEqual([ORG, BOB]);
});

it("fails when no session can mint one", async () => {
  fakeDelegation("did:web:nobody.example");
  await expect(issueDelegation(env, DOC)).rejects.toThrow(
    `no session can access ${DOC}`,
  );
});
