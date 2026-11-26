import { env } from "cloudflare:workers";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { getDb } from "@/db";
import { apps } from "@/db/schema";
import { encodeBundle } from "@/server/apps/apps.server";
import { runApp, workerName } from "@/server/apps/runner";

// Runs real Dynamic Workers through the LOADER binding; only the read of
// the code bundle out of pear is faked.

const ORG = "did:web:org.example";
const SPACE = `at:space/${ORG}/network.habitat.apps/3kapp`;
const URI = `${SPACE}/${ORG}/network.habitat.apps.app/self`;
const HOST = "chat-3kapp";
const RUN = `https://${HOST}.${env.APP_RUN_DOMAIN}`;

const db = getDb(env);
let blobs: Map<string, Uint8Array>;
const getBlob = vi.fn(async (_space: string, _repo: string, cid: string) => {
  const bytes = blobs.get(cid);
  if (!bytes) throw new Error("BlobNotFound");
  return bytes;
});
const deps = { getBlob };

let deploys = 0;
async function deploy(
  modules: Record<string, string>,
  entrypoint = "index.js",
) {
  deploys++;
  const codeCid = `bafkcode${deploys}`;
  blobs.set(codeCid, encodeBundle({ modules }));
  const row = {
    uri: URI,
    cid: `bafyrecord${deploys}`,
    space: SPACE,
    repo: ORG,
    orgDid: ORG,
    host: HOST,
    name: "Chat",
    slug: "chat",
    entrypoint,
    codeCid,
    codeSize: 1,
    version: deploys,
    compatibilityDate: "2026-08-01",
    createdAt: 1,
    updatedAt: deploys,
  };
  await db
    .insert(apps)
    .values(row)
    .onConflictDoUpdate({ target: apps.uri, set: row });
}

beforeEach(async () => {
  blobs = new Map();
  getBlob.mockClear();
  await db.delete(apps);
});

describe("runApp", () => {
  it("answers a request with the app's code", async () => {
    await deploy({
      "index.js": `import page from "./page.html"; import cfg from "./cfg.json";
        export default { fetch(req, env) {
          const url = new URL(req.url);
          if (url.pathname === "/env") return Response.json(env.HABITAT);
          return new Response(page.replace("{{greeting}}", cfg.greeting), {
            headers: { "content-type": "text/html" } });
        } };`,
      "page.html": "<h1>{{greeting}}</h1>",
      "cfg.json": `{"greeting": "hello"}`,
    });

    const res = await runApp(new Request(`${RUN}/`), env, deps);
    expect(res.status).toBe(200);
    expect(await res.text()).toBe("<h1>hello</h1>");

    const info = await (
      await runApp(new Request(`${RUN}/env`), env, deps)
    ).json();
    expect(info).toEqual({
      app: URI,
      org: ORG,
      name: "Chat",
      version: 1,
      url: `https://${HOST}.${env.APP_RUN_DOMAIN}/`,
    });
  });

  it("gives the app no network and none of the Habitat app's bindings", async () => {
    await deploy({
      "index.js": `export default { async fetch(req, env) {
          let network;
          try { await fetch("https://example.com"); network = "allowed"; }
          catch (e) { network = "blocked"; }
          return Response.json({ network, keys: Object.keys(env) });
        } };`,
    });
    const body = await (await runApp(new Request(`${RUN}/`), env, deps)).json();
    expect(body).toEqual({ network: "blocked", keys: ["HABITAT"] });
  });

  it("serves a new deploy's code, keeping each version's isolate", async () => {
    await deploy({
      "index.js": `export default { fetch() { return new Response("v1") } };`,
    });
    expect(await (await runApp(new Request(RUN), env, deps)).text()).toBe("v1");
    expect(await (await runApp(new Request(RUN), env, deps)).text()).toBe("v1");

    await deploy(
      {
        "main.js": `export default { fetch() { return new Response("v2") } };`,
      },
      "main.js",
    );
    expect(await (await runApp(new Request(RUN), env, deps)).text()).toBe("v2");
  });

  it("says when there's no app at the address", async () => {
    const res = await runApp(
      new Request(`https://nope-3k.${env.APP_RUN_DOMAIN}/`),
      env,
      deps,
    );
    expect(res.status).toBe(404);
    const bare = await runApp(
      new Request(`https://${env.APP_RUN_DOMAIN}/`),
      env,
      deps,
    );
    expect(bare.status).toBe(404);
  });

  it("answers 500 when the app throws", async () => {
    await deploy({
      "index.js": `export default { fetch() { throw new Error("boom") } };`,
    });
    const res = await runApp(new Request(RUN), env, deps);
    expect(res.status).toBe(500);
    expect(await res.text()).toContain("This app failed");
  });

  it("names each isolate by the record version it runs", () => {
    expect(workerName({ uri: URI, cid: "bafy1" })).toBe(`${URI}@bafy1`);
  });
});
