import { env } from "cloudflare:test";
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";
import worker from "../src/server/entry";
import { connectedOrgNames, getDb } from "../src/db";

// These drive /session/callback and /session/org-callback end to end
// through the worker's own fetch handler, so they exercise the routes' real
// behavior: whatever the browser puts in the callback URL, a chalk_session
// cookie must only be issued for — and an org only connected for — a DID
// sap vouches for.

const SAP = env.CHALK_SAP_INTERNAL_URL;

const server = setupServer();
beforeAll(() => {
  process.env.CHALK_SESSION_SECRET = "test-secret-at-least-32-characters!!";
});
beforeEach(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  server.close();
});

// fakeSap stands in for sap's internal port: each code in codes redeems
// exactly once for its DID (the contract of cmd/sap/server.go's
// handleExchangeCode), recrawl always succeeds, and every org's profile is
// readable (as "Example Org") — so connecting an org is only ever gated on
// the code, which is what the org-callback tests below exercise.
function fakeSap(codes: Map<string, string>) {
  server.use(
    http.post(`${SAP}/session/exchange`, async ({ request }) => {
      const { code } = (await request.json()) as { code: string };
      const did = codes.get(code);
      if (!did) {
        return new HttpResponse("unknown or expired code", { status: 404 });
      }
      codes.delete(code);
      return HttpResponse.json({ did });
    }),
    http.post(
      `${SAP}/session/recrawl`,
      () => new HttpResponse(null, { status: 202 }),
    ),
    http.get(`${SAP}/proxy/network.habitat.space.getRecord`, () =>
      HttpResponse.json({ value: { name: "Example Org" } }),
    ),
  );
}

function callback(query: string): Promise<Response> {
  return worker.fetch(
    new Request(`https://chalk.test/session/callback?${query}`),
  );
}

function orgCallback(query: string, cookie: string): Promise<Response> {
  return worker.fetch(
    new Request(`https://chalk.test/session/org-callback?${query}`, {
      headers: { cookie },
    }),
  );
}

// signIn completes a real /session/callback round trip with a sap-issued
// code and returns the resulting session cookie, ready to send back.
async function signIn(codes: Map<string, string>): Promise<string> {
  codes.set("login-code", "did:plc:member1");
  const res = await callback("code=login-code");
  const cookie = res.headers
    .getSetCookie()
    .find((c) => c.startsWith("chalk_session="));
  expect(cookie).toBeDefined();
  return cookie!.split(";")[0];
}

// pageText returns res's HTML with the `<!-- -->` markers React's SSR puts
// between adjacent text nodes (e.g. "approved Chalk with <!-- -->{name}")
// removed, so assertions can match the text as a reader sees it.
async function pageText(res: Response): Promise<string> {
  return (await res.text()).replaceAll("<!-- -->", "");
}

async function isConnected(orgDid: string): Promise<boolean> {
  const names = await connectedOrgNames(getDb(env), [orgDid]);
  return names.has(orgDid);
}

function setsSessionCookie(res: Response): boolean {
  return res.headers.getSetCookie().some((c) => c.startsWith("chalk_session="));
}

// redirectPath asserts res is a redirect (so a rendering error can't pass
// for "no session was created") and returns where it points.
function redirectPath(res: Response): string {
  expect(res.status).toBeGreaterThanOrEqual(300);
  expect(res.status).toBeLessThan(400);
  return new URL(res.headers.get("location")!, "https://chalk.test").pathname;
}

// The first request through the worker entry compiles the whole SSR route
// tree, which takes several seconds — well past vitest's 5s default.
describe("/session/callback", { timeout: 60_000 }, () => {
  it("does not create a session from a forged ?did=", async () => {
    fakeSap(new Map());
    const res = await callback("did=did:plc:victim");
    expect(redirectPath(res)).toBe("/login");
    expect(setsSessionCookie(res)).toBe(false);
  });

  it("creates a session for a sap-issued code, only once", async () => {
    fakeSap(new Map([["good-code", "did:plc:member1"]]));

    const first = await callback("code=good-code");
    expect(redirectPath(first)).toBe("/");
    expect(setsSessionCookie(first)).toBe(true);

    const second = await callback("code=good-code");
    expect(redirectPath(second)).toBe("/login");
    expect(setsSessionCookie(second)).toBe(false);
  });
});

// Each test uses its own org DID so D1 state from one can't satisfy another.
describe("/session/org-callback", { timeout: 60_000 }, () => {
  it("does not connect an org from a forged ?did=", async () => {
    const org = "did:web:forged.example";
    const codes = new Map<string, string>();
    fakeSap(codes);
    const cookie = await signIn(codes);

    const res = await orgCallback(`did=${org}`, cookie);
    expect(res.status).toBe(200);
    expect(await pageText(res)).not.toContain("Successfully approved Chalk");
    expect(await isConnected(org)).toBe(false);
  });

  it("connects the org for a sap-issued code, only once", async () => {
    const org = "did:web:real.example";
    const codes = new Map([["org-code", org]]);
    fakeSap(codes);
    const cookie = await signIn(codes);

    const first = await orgCallback("code=org-code", cookie);
    expect(first.status).toBe(200);
    expect(await pageText(first)).toContain(
      "Successfully approved Chalk with Example Org",
    );
    expect(await isConnected(org)).toBe(true);

    const second = await orgCallback("code=org-code", cookie);
    expect(second.status).toBe(200);
    expect(await pageText(second)).toContain("connect this org");
  });
});
