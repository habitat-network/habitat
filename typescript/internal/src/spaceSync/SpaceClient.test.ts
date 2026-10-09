// @vitest-environment node
import { it } from "@effect/vitest";
import { cidForRawBytes } from "@atproto/lex";
import { Duration, Effect, Layer } from "effect";
import { HttpResponse, delay, http } from "msw";
import { describe, expect } from "vitest";
import { server } from "../test/msw";
import { Credentials } from "./Credentials";
import { Identity } from "./Identity";
import { SpaceClient } from "./SpaceClient";
import { FakeNetwork } from "./test/fakeNetwork";
import { fakeDelegation, testConfig } from "./test/harness";

const setupWith = (overrides: Parameters<typeof testConfig>[0] = {}) =>
  Effect.promise(async () => {
    const net = new FakeNetwork();
    server.use(...net.handlers);
    const alice = await net.createAccount("alice");
    const bob = await net.createAccount("bob");
    const space = net.createSpace(alice);
    const layer = SpaceClient.layer.pipe(
      Layer.provideMerge(Credentials.layer),
      Layer.provideMerge(Identity.layer),
      Layer.provide(fakeDelegation(net)),
      Layer.provide(testConfig(overrides)),
    );
    return { net, alice, bob, space, layer };
  });
const setup = setupWith();

describe("SpaceClient", () => {
  it.live("lists writers in spaceRev order with a matching cursor", () =>
    Effect.gen(function* () {
      const { alice, bob, space, layer } = yield* setup;
      yield* Effect.promise(() =>
        space.write(bob, "com.example.post", "1", { text: "b" }),
      );
      yield* Effect.promise(() =>
        space.write(alice, "com.example.post", "1", { text: "a" }),
      );
      const page = yield* Effect.flatMap(SpaceClient, (c) =>
        c.listRepos(space.ref),
      ).pipe(Effect.provide(layer));
      expect(page.repos.map((r) => r.did)).toEqual([bob.did, alice.did]);
      expect(page.cursor).toBe(space.spaceRevOf(alice.did));
    }),
  );

  it.live("pages listRepoOps and returns the commit on the last page", () =>
    Effect.gen(function* () {
      const { bob, space, layer } = yield* setup;
      for (const rkey of ["1", "2", "3"]) {
        yield* Effect.promise(() =>
          space.write(bob, "com.example.post", rkey, { text: rkey }),
        );
      }
      space.maxOpsPage = 2;
      yield* Effect.gen(function* () {
        const client = yield* SpaceClient;
        const first = yield* client.listRepoOps(space.ref, bob.did, undefined);
        expect(first.ops).toHaveLength(2);
        expect(first.commit).toBeUndefined();
        const second = yield* client.listRepoOps(
          space.ref,
          bob.did,
          undefined,
          first.cursor,
        );
        expect(second.ops).toHaveLength(1);
        expect(second.commit?.rev).toBe(space.repoRevOf(bob.did));
      }).pipe(Effect.provide(layer));
    }),
  );

  it.live(
    "re-mints and retries once when a host reports CredentialRevoked",
    () =>
      Effect.gen(function* () {
        const { bob, space, layer } = yield* setup;
        yield* Effect.promise(() =>
          space.write(bob, "com.example.post", "1", { text: "x" }),
        );
        yield* Effect.gen(function* () {
          const client = yield* SpaceClient;
          yield* client.listRepos(space.ref);
          space.revokeAllCredentials();
          yield* client.listRepoOps(space.ref, bob.did, undefined);
          expect(space.credentialJtis).toHaveLength(2);
        }).pipe(Effect.provide(layer));
      }),
  );

  it.live("surfaces SpaceDeleted as a CredentialError", () =>
    Effect.gen(function* () {
      const { space, layer } = yield* setup;
      space.deleted = true;
      const error = yield* Effect.flip(
        Effect.flatMap(SpaceClient, (c) => c.listRepos(space.ref)),
      ).pipe(Effect.provide(layer));
      expect(error._tag).toBe("CredentialError");
      expect(error._tag === "CredentialError" && error.reason).toBe(
        "SpaceDeleted",
      );
    }),
  );

  it.live("retries 5xx responses then fails with the last XrpcError", () =>
    Effect.gen(function* () {
      const { bob, space, layer } = yield* setup;
      yield* Effect.promise(() =>
        space.write(bob, "com.example.post", "1", { text: "x" }),
      );
      space.failingRepos.add(bob.did);
      const error = yield* Effect.flip(
        Effect.flatMap(SpaceClient, (c) =>
          c.listRepoOps(space.ref, bob.did, undefined),
        ),
      ).pipe(Effect.provide(layer));
      expect(error._tag === "XrpcError" && error.status).toBe(500);
      expect(space.listRepoOpsCalls).toBe(3);
    }),
  );

  it.live("streams getRepo bytes", () =>
    Effect.gen(function* () {
      const { bob, space, layer } = yield* setup;
      yield* Effect.promise(() =>
        space.write(bob, "com.example.post", "1", { text: "x" }),
      );
      const size = yield* Effect.scoped(
        Effect.gen(function* () {
          const body = yield* Effect.flatMap(SpaceClient, (c) =>
            c.getRepo(space.ref, bob.did),
          );
          let n = 0;
          yield* Effect.promise(async () => {
            for await (const chunk of body) n += chunk.length;
          });
          return n;
        }),
      ).pipe(Effect.provide(layer));
      expect(size).toBeGreaterThan(0);
    }),
  );

  it.live("fails a request that gets no response within requestTimeout", () =>
    Effect.gen(function* () {
      const { space, layer } = yield* setupWith({
        requestTimeout: Duration.millis(50),
        requestRetries: 0,
      });
      server.use(
        http.get("*/xrpc/com.atproto.space.listRepos", async () => {
          await delay("infinite");
          return HttpResponse.json({ repos: [] });
        }),
      );
      const error = yield* Effect.flip(
        Effect.flatMap(SpaceClient, (c) => c.listRepos(space.ref)).pipe(
          Effect.provide(layer),
        ),
      );
      expect(error._tag === "XrpcError" && error.error).toBe("Timeout");
    }),
  );

  it.live("rejects a blob whose bytes don't match its cid", () =>
    Effect.gen(function* () {
      const { bob, space, layer } = yield* setupWith({ requestRetries: 0 });
      const real = new TextEncoder().encode("real blob");
      const cid = (yield* Effect.promise(() =>
        cidForRawBytes(real),
      )).toString();
      let served: Uint8Array = real;
      server.use(
        http.get(
          "*/xrpc/com.atproto.space.getBlob",
          () =>
            new HttpResponse(served, {
              headers: { "content-type": "application/octet-stream" },
            }),
        ),
      );
      const getBlob = Effect.flatMap(SpaceClient, (c) =>
        c.getBlob(space.ref, bob.did, cid),
      ).pipe(Effect.provide(layer));
      expect(yield* getBlob).toEqual(real);
      served = new TextEncoder().encode("forged blob");
      const error = yield* Effect.flip(getBlob);
      expect(error._tag === "XrpcError" && error.error).toBe("InvalidResponse");
    }),
  );

  it.live("only trusts SpaceDeleted from the space host", () =>
    Effect.gen(function* () {
      const { bob, space, layer } = yield* setupWith({ requestRetries: 0 });
      yield* Effect.promise(() =>
        space.write(bob, "com.example.post", "1", { text: "b" }),
      );
      space.repoHostErrors.set(bob.did, "SpaceDeleted");
      const error = yield* Effect.flip(
        Effect.flatMap(SpaceClient, (c) =>
          c.listRepoOps(space.ref, bob.did, undefined),
        ).pipe(Effect.provide(layer)),
      );
      expect(error._tag).toBe("XrpcError");
    }),
  );
});
