// @vitest-environment node
import { it } from "@effect/vitest";
import { P256Keypair } from "@atproto/crypto";
import { cidForRawBytes } from "@atproto/lex";
import { type Commit, verifyCommit } from "@atproto/space";
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

  it.live(
    "pages listRepoOps and returns the MACed commit on the last page",
    () =>
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
          const first = yield* client.listRepoOps(
            space.ref,
            bob.did,
            undefined,
          );
          expect(first.body.ops).toHaveLength(2);
          expect(first.body.commit).toBeUndefined();
          expect(first.mac).toBeUndefined();
          const second = yield* client.listRepoOps(
            space.ref,
            bob.did,
            undefined,
            first.body.cursor,
          );
          expect(second.body.ops).toHaveLength(1);
          const commit = second.body.commit!;
          expect(commit.rev).toBe(space.repoRevOf(bob.did));
          // The MAC is keyed by the secret negotiated with bob's #atproto_kem.
          const mac = second.mac!;
          expect(
            verifyCommit(commit as Commit, {
              space: space.id,
              repo: bob.did,
              sharedSecret: mac.sharedSecret,
              mac: mac.mac!,
            }),
          ).toBe(true);
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

  it.live("refreshKem picks up a rotated KEM key", () =>
    Effect.gen(function* () {
      const { bob, space, layer } = yield* setup;
      yield* Effect.promise(() =>
        space.write(bob, "com.example.post", "1", { text: "x" }),
      );
      yield* Effect.gen(function* () {
        const client = yield* SpaceClient;
        // Resolve and cache bob's current KEM key.
        yield* client.listRepoOps(space.ref, bob.did, undefined);
        bob.kem = yield* Effect.promise(() => P256Keypair.create());
        const calls = space.listRepoOpsCalls;
        const page = yield* client.listRepoOps(space.ref, bob.did, undefined);
        // The stale key share's MAC fails to verify, so refreshing finds the
        // new key and a retry would succeed.
        expect(
          verifyCommit(page.body.commit as Commit, {
            space: space.id,
            repo: bob.did,
            sharedSecret: page.mac!.sharedSecret,
            mac: page.mac!.mac!,
          }),
        ).toBe(false);
        expect(yield* client.refreshKem(space.ref, bob.did)).toBe(true);
        expect(yield* client.refreshKem(space.ref, bob.did)).toBe(false);
        const fresh = yield* client.listRepoOps(space.ref, bob.did, undefined);
        expect(
          verifyCommit(fresh.body.commit as Commit, {
            space: space.id,
            repo: bob.did,
            sharedSecret: fresh.mac!.sharedSecret,
            mac: fresh.mac!.mac!,
          }),
        ).toBe(true);
        expect(space.listRepoOpsCalls).toBe(calls + 2);
      }).pipe(Effect.provide(layer));
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
          const { star, mac } = yield* Effect.flatMap(SpaceClient, (c) =>
            c.getRepo(space.ref, bob.did),
          );
          expect(mac.mac).toBeInstanceOf(Uint8Array);
          let n = 0;
          yield* Effect.promise(async () => {
            for await (const chunk of star) n += chunk.length;
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
