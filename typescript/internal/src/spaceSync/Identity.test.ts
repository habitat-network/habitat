// @vitest-environment node
import { it } from "@effect/vitest";
import { Secp256k1Keypair } from "@atproto/crypto";
import type { DidDocument } from "@atproto/identity";
import { Effect, Exit, Layer } from "effect";
import { HttpResponse, http } from "msw";
import { describe, expect } from "vitest";
import { server } from "../test/msw";
import { spaceSyncConfigLayer } from "./config";
import { Identity, identityFromDoc } from "./Identity";

const DID = "did:plc:aliceaaaaaaaaaaaaaaaaaaa";

const docWith = async (
  extra: Partial<DidDocument> = {},
): Promise<DidDocument> => {
  const key = await Secp256k1Keypair.create();
  return {
    id: DID,
    verificationMethod: [
      {
        id: `${DID}#atproto`,
        type: "Multikey",
        controller: DID,
        publicKeyMultibase: key.did().slice("did:key:".length),
      },
    ],
    service: [
      {
        id: "#atproto_pds",
        type: "AtprotoPersonalDataServer",
        serviceEndpoint: "https://pds.test",
      },
    ],
    ...extra,
  } as DidDocument;
};

describe("identityFromDoc", () => {
  it.effect("falls back to the PDS when there is no #atproto_space_host", () =>
    Effect.gen(function* () {
      const doc = yield* Effect.promise(() => docWith());
      const id = yield* identityFromDoc(DID, doc);
      expect(id.pds).toBe("https://pds.test");
      expect(id.spaceHost).toBe("https://pds.test");
      expect(id.signingKey).toMatch(/^did:key:z/);
    }),
  );

  it.effect("uses a declared #atproto_space_host", () =>
    Effect.gen(function* () {
      const base = yield* Effect.promise(() => docWith());
      const doc = {
        ...base,
        service: [
          ...(base.service ?? []),
          {
            id: "#atproto_space_host",
            type: "AtprotoSpaceHost",
            serviceEndpoint: "https://spaces.test",
          },
        ],
      } as DidDocument;
      expect((yield* identityFromDoc(DID, doc)).spaceHost).toBe(
        "https://spaces.test",
      );
    }),
  );

  it.effect(
    "errors instead of falling back when #atproto_space_host is malformed",
    () =>
      Effect.gen(function* () {
        const base = yield* Effect.promise(() => docWith());
        const doc = {
          ...base,
          service: [
            ...(base.service ?? []),
            {
              id: "#atproto_space_host",
              type: "Wrong",
              serviceEndpoint: "https://spaces.test",
            },
          ],
        } as DidDocument;
        const exit = yield* Effect.exit(identityFromDoc(DID, doc));
        expect(Exit.isFailure(exit)).toBe(true);
      }),
  );
});

describe("Identity.layer", () => {
  it.effect(
    "resolves did:plc through the configured PLC directory and caches",
    () =>
      Effect.gen(function* () {
        const doc = yield* Effect.promise(() => docWith());
        let hits = 0;
        server.use(
          http.get("https://plc.test/:did", () => {
            hits++;
            return HttpResponse.json(doc);
          }),
        );
        const identity = yield* Identity;
        expect((yield* identity.resolve(DID)).pds).toBe("https://pds.test");
        yield* identity.resolve(DID);
        expect(hits).toBe(1);
      }).pipe(
        Effect.provide(
          Identity.layer.pipe(
            Layer.provide(
              spaceSyncConfigLayer({
                serviceDid: "did:web:s.test",
                plcUrl: "https://plc.test",
              }),
            ),
          ),
        ),
      ),
  );

  it.effect("resolves did:web and refuses redirects", () =>
    Effect.gen(function* () {
      const web = "did:web:alice.test";
      const key = yield* Effect.promise(() => Secp256k1Keypair.create());
      const doc = yield* Effect.promise(() =>
        docWith({
          id: web,
          verificationMethod: [
            {
              id: `${web}#atproto`,
              type: "Multikey",
              controller: web,
              publicKeyMultibase: key.did().slice("did:key:".length),
            },
          ],
        }),
      );
      server.use(
        http.get("https://alice.test/.well-known/did.json", () =>
          HttpResponse.json(doc),
        ),
        http.get(
          "https://moved.test/.well-known/did.json",
          () =>
            new HttpResponse(null, {
              status: 302,
              headers: { location: "https://alice.test/.well-known/did.json" },
            }),
        ),
      );
      const identity = yield* Identity;
      expect((yield* identity.resolve(web as never)).pds).toBe(
        "https://pds.test",
      );
      const exit = yield* Effect.exit(
        identity.resolve("did:web:moved.test" as never),
      );
      expect(Exit.isFailure(exit)).toBe(true);
    }).pipe(
      Effect.provide(
        Identity.layer.pipe(
          Layer.provide(
            spaceSyncConfigLayer({
              serviceDid: "did:web:s.test",
              plcUrl: "https://plc.test",
            }),
          ),
        ),
      ),
    ),
  );
});
