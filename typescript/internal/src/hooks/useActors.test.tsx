import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HttpResponse, http } from "msw";
import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import type { PropsWithChildren } from "react";
import { server } from "../test/msw";
import { useActors } from "./useActors";
import type { Actor } from "../types/Actor";

const GET_PROFILES =
  "https://public.api.bsky.app/xrpc/app.bsky.actor.getProfiles";

const alice: Actor = {
  did: "did:plc:alice",
  handle: "alice.test",
  displayName: "Alice",
};

const bob: Actor = {
  did: "did:plc:bob",
  handle: "bob.test",
  displayName: "Bob",
  avatar: "https://cdn.test/bob.jpg",
};

const carol: Actor = {
  did: "did:plc:carol",
  handle: "carol.test",
  displayName: "Carol",
};

const dave: Actor = {
  did: "did:plc:dave",
  handle: "dave.test",
};

const actors = [alice, bob, carol, dave];

const profilesByDid = new Map<string, Actor>();
let requests: string[][] = [];

beforeEach(() => {
  profilesByDid.clear();
  requests = [];
  server.use(
    http.get(GET_PROFILES, ({ request }) => {
      const dids = new URL(request.url).searchParams.getAll("actors");
      requests.push(dids);
      const profiles = dids
        .map((did) => profilesByDid.get(did))
        .filter((profile): profile is Actor => profile !== undefined);
      return HttpResponse.json({ profiles });
    }),
  );
});

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });
  return {
    queryClient,
    wrapper: ({ children }: PropsWithChildren) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  };
}

describe("useActors", () => {
  it("resolves known DIDs to their full profiles", async () => {
    profilesByDid.set(alice.did, alice);
    const { wrapper } = createWrapper();

    const { result } = renderHook(() => useActors([alice.did]), { wrapper });

    await waitFor(() => expect(result.current(alice.did)).toEqual(alice));
  });

  it("falls back to a bare { did } when getProfiles omits the DID", async () => {
    const ghost = "did:plc:ghost";
    const { wrapper } = createWrapper();

    const { result } = renderHook(() => useActors([ghost]), { wrapper });

    await waitFor(() => expect(result.current(ghost)).toEqual({ did: ghost }));
  });

  it("resolves multiple DIDs to their individual profiles in one batch", async () => {
    for (const actor of actors) {
      profilesByDid.set(actor.did, actor);
    }
    const dids = actors.map((actor) => actor.did);
    const { wrapper } = createWrapper();

    const { result } = renderHook(() => useActors(dids), { wrapper });

    await waitFor(() =>
      expect(result.current(dids[dids.length - 1])).toEqual(
        actors[dids.length - 1],
      ),
    );
    for (const actor of actors) {
      expect(result.current(actor.did)).toEqual(actor);
    }
    expect(requests).toHaveLength(1);
    expect([...requests[0]].sort()).toEqual([...dids].sort());
  });

  it("coalesces same-tick lookups into a single getProfiles request", async () => {
    const dids = ["did:plc:one", "did:plc:two", "did:plc:three"];
    for (const did of dids) {
      profilesByDid.set(did, { did });
    }
    const { wrapper } = createWrapper();

    const { result } = renderHook(() => useActors(dids), { wrapper });

    await waitFor(() => {
      expect(result.current(dids[2])).toEqual({ did: dids[2] });
    });
    expect(requests).toHaveLength(1);
    expect([...requests[0]].sort()).toEqual([...dids].sort());
  });

  it("dedupes duplicate DIDs before querying", async () => {
    profilesByDid.set(alice.did, alice);
    const { wrapper } = createWrapper();

    renderHook(() => useActors([alice.did, alice.did, alice.did]), {
      wrapper,
    });

    await waitFor(() => expect(requests).toHaveLength(1));
    expect(requests[0]).toEqual([alice.did]);
  });

  it("reuses cached profiles when re-listing a subset of DIDs", async () => {
    const dids = ["did:plc:one", "did:plc:two"];
    for (const did of dids) {
      profilesByDid.set(did, { did, handle: did });
    }
    const { wrapper } = createWrapper();

    const first = renderHook(() => useActors(dids), { wrapper });
    await waitFor(() =>
      expect(first.result.current(dids[0])).toEqual({
        did: dids[0],
        handle: dids[0],
      }),
    );

    const second = renderHook(() => useActors([dids[0]]), { wrapper });
    await waitFor(() =>
      expect(second.result.current(dids[0])).toEqual({
        did: dids[0],
        handle: dids[0],
      }),
    );

    expect(requests).toHaveLength(1);
  });

  it("falls back to a bare { did } when getProfiles fails", async () => {
    server.use(http.get(GET_PROFILES, () => HttpResponse.error()));
    const { wrapper } = createWrapper();

    const { result } = renderHook(() => useActors([alice.did]), { wrapper });

    await waitFor(() =>
      expect(result.current(alice.did)).toEqual({ did: alice.did }),
    );
  });

  it("issues no requests for an empty DID list", () => {
    const { wrapper } = createWrapper();

    const { result } = renderHook(() => useActors([]), { wrapper });

    expect(result.current("did:plc:any")).toEqual({ did: "did:plc:any" });
    expect(requests).toHaveLength(0);
  });
});
