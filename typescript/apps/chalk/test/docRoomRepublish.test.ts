import {
  env,
  runInDurableObject,
  runDurableObjectAlarm,
} from "cloudflare:test";
import { beforeEach, expect, it, vi } from "vitest";
import * as Y from "yjs";
import type { DocRoom } from "../src/server/rooms/docRoom";
import { getDb, docByUri, upsertDoc } from "../src/db";

const URI = "at://did:web:alice.example/space/network.habitat.docs/pub";

const fetchMock = vi.fn();
beforeEach(async () => {
  fetchMock.mockReset();
  fetchMock.mockImplementation(
    async () =>
      new Response(JSON.stringify({ blob: { ref: { $link: "c" } }, cid: "c" })),
  );
  vi.stubGlobal("fetch", fetchMock);
  await env.DB.exec("DELETE FROM docs");
});

// renderDoc walks ydoc.getXmlFragment("default") (see src/render.ts), which
// is what TipTap's Collaboration extension writes into. A getText() fixture
// would leave that fragment empty and the title would always be "Untitled",
// so the fixture must build the XML fragment renderDoc actually reads.
function headingUpdate(text: string): Uint8Array {
  const d = new Y.Doc();
  const heading = new Y.XmlElement("heading");
  heading.setAttribute("level", "1");
  heading.insert(0, [new Y.XmlText(text)]);
  d.getXmlFragment("default").insert(0, [heading]);
  return Y.encodeStateAsUpdateV2(d);
}

it("republishes crdt and markdown under the owner's repo", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      { spaceUri: URI, ownerDid: "did:web:alice.example" },
      "did:web:bob.example",
      headingUpdate("Title"),
    ),
  );
  await new Promise((resolve) => setTimeout(resolve, 2100));
  await runDurableObjectAlarm(stub);
  const puts = fetchMock.mock.calls
    .filter((c) => String(c[0]).includes("space.putRecord"))
    .map(
      (c) =>
        JSON.parse(String(c[1].body)) as {
          space: string;
          repo: string;
          collection: string;
        },
    );
  const ownerPuts = puts.filter(
    (p) => p.space === URI && p.repo === "did:web:alice.example",
  );
  expect(ownerPuts.map((p) => p.collection).sort()).toEqual([
    "network.habitat.docs.crdt",
    "network.habitat.docs.markdown",
  ]);
});

it("skips republish when the owner is unknown", async () => {
  const uri = URI + "-2";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit({ spaceUri: uri }, "did:web:bob.example", headingUpdate("x")),
  );
  await new Promise((resolve) => setTimeout(resolve, 2100));
  await runDurableObjectAlarm(stub);
  const repos = fetchMock.mock.calls
    .filter((c) => String(c[0]).includes("space.putRecord"))
    .map(
      (c) => JSON.parse(String(c[1].body)) as { space: string; repo: string },
    )
    .filter((body) => body.space === uri)
    .map((body) => body.repo);
  expect(repos).toEqual(["did:web:bob.example"]);
});

it("writes the rendered title back to the D1 index", async () => {
  const uri = URI + "-3";
  await upsertDoc(getDb(env), {
    spaceUri: uri,
    docId: uri,
    ownerDid: "did:web:alice.example",
    title: "Untitled",
  });
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      { spaceUri: uri, ownerDid: "did:web:alice.example" },
      "did:web:bob.example",
      headingUpdate("Hello"),
    ),
  );
  await new Promise((resolve) => setTimeout(resolve, 2100));
  await runDurableObjectAlarm(stub);
  expect((await docByUri(getDb(env), uri))?.title).toBe("Hello");
});
