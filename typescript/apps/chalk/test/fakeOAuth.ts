// Test stand-in for src/server/oauth.ts, swapped in with
// `vi.mock("../src/server/oauth", () => import("./fakeOAuth"))`. restore()
// hands back a session-like agent whose fetchHandler sends each call to PDS
// over the global fetch — which tests stub or intercept with msw — tagged
// with the DID it was made as, so a test can tell whose session a call used.
// authorize() records its calls on `authorize` (a vi.fn) for startLogin's
// tests to inspect or make fail.

import { vi } from "vitest";

export const PDS = "http://pds.test";
export const TEST_DID_HEADER = "X-Test-Did";
export const OAUTH_SCOPE = "atproto";

export const authorize = vi.fn(
  async (_hint: string, _opts?: unknown) =>
    new URL("https://pear.test/oauth/authorize"),
);

// refreshOrgSession stands in for the JWT bearer refresh: it reports
// whether a fresh session was minted. A test that wants it to succeed sets
// `refreshed` to the DIDs it succeeds for; it then clears `lost`, as a
// fresh session would.
export const refreshOrgSession = vi.fn(async (_env: unknown, did: string) => {
  if (!refreshed.has(did)) return false;
  lost.delete(did);
  return true;
});
// refreshed holds the DIDs refreshOrgSession succeeds for; lost holds the
// DIDs whose session restore() can't find until they're refreshed.
export const refreshed = new Set<string>();
export const lost = new Set<string>();

export function createOAuthClient() {
  return {
    authorize,
    restore: async (did: string) => {
      if (lost.has(did)) throw new Error("session not found");
      return {
        did,
        fetchHandler: (path: string, init: RequestInit = {}) => {
          const headers = new Headers(init.headers);
          headers.set(TEST_DID_HEADER, did);
          return fetch(`${PDS}${path}`, { ...init, headers });
        },
      };
    },
  };
}

export function clientMetadata() {
  return {};
}
