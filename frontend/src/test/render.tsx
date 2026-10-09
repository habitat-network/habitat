import {
  RouterProvider,
  createMemoryHistory,
  createRouter,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import type { AuthManager } from "internal";
import { routeTree } from "@/routeTree.gen";
import { PEAR, server } from "./server";

export const TEST_DID = "did:plc:alice";
export const HABITAT_DID = "did:web:pear.test";

// fakeAuthManager stands in for the OAuth-backed AuthManager: it satisfies the
// same Agent interface but sends requests straight to the (msw-mocked) pear
// origin, so xrpc() calls hit the handlers a test registers.
// Pass null for a signed-out user.
export function fakeAuthManager(did: string | null = TEST_DID) {
  const send = (path: string, init?: RequestInit) =>
    fetch(`${PEAR}${path}`, init);
  return {
    did: did ?? undefined,
    init: async () => {},
    getAuthInfo: () => (did ? { did } : undefined),
    login: async () => {},
    logout: () => {},
    fetchHandler: (path: string, init: RequestInit) => send(path, init),
    fetch: (
      path: string,
      method = "GET",
      body?: BodyInit | null,
      headers?: Headers,
    ) => send(path, { method, body, headers }),
  } as unknown as AuthManager;
}

// mockHabitatInstance registers the handlers every page built on pearAgent
// needs: the did:web document that resolves the instance's #habitat service,
// and the service-auth token mint.
export function mockHabitatInstance() {
  server.use(
    http.get(`${PEAR}/.well-known/did.json`, () =>
      HttpResponse.json({
        "@context": ["https://www.w3.org/ns/did/v1"],
        id: HABITAT_DID,
        service: [
          {
            id: "#habitat",
            type: "HabitatServer",
            serviceEndpoint: PEAR,
          },
        ],
      }),
    ),
    http.get(`${PEAR}/xrpc/com.atproto.server.getServiceAuth`, () =>
      HttpResponse.json({ token: "test-service-token" }),
    ),
    // Org profiles are read through a space credential; fail the exchange so
    // pages fall back to showing the bare DID.
    http.get(`${PEAR}/xrpc/com.atproto.space.getDelegationToken`, () =>
      HttpResponse.json({ error: "NotFound" }, { status: 404 }),
    ),
    // Root route loader looks the signed-in user's profile up here; fail it
    // so the app falls back to the bare DID.
    http.get("https://public.api.bsky.app/xrpc/app.bsky.actor.getProfile", () =>
      HttpResponse.json({ error: "NotFound" }, { status: 400 }),
    ),
  );
}

// xrpcHandler mocks one XRPC endpoint on the pear origin. `method` defaults
// to query (GET); pass "post" for procedures.
export function xrpcHandler(
  nsid: string,
  resolver: Parameters<typeof http.get>[1],
  method: "get" | "post" = "get",
) {
  return http[method](`${PEAR}/xrpc/${nsid}`, resolver);
}

// renderApp renders the real route tree at `path` inside the providers the
// app's main.tsx sets up (query client, router with auth context), with an
// in-memory history. Resolves once the initial route has loaded.
export async function renderApp(
  path: string,
  { authManager = fakeAuthManager() }: { authManager?: AuthManager } = {},
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const router = createRouter({
    routeTree,
    context: { queryClient, authManager },
    history: createMemoryHistory({ initialEntries: [path] }),
    defaultPendingMinMs: 0,
  });
  await router.load();
  const result = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { ...result, router, queryClient, authManager };
}
