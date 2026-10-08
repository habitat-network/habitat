import { P256Keypair } from "@atproto/crypto";
import { createSpaceSigHeaders, parseSpaceToken, spaceHostAud } from "@atproto/space";
import { Cache, Clock, Context, Duration, Effect, Exit, Layer, Predicate, Schema } from "effect";
import { SpaceSyncConfig } from "./config";
import { CredentialError, errorMessage } from "./errors";
import { Identity } from "./Identity";
import { GetSpaceCredentialOutput, parseSpaceRef } from "./wire";

export interface SpaceCredential {
  readonly space: string;
  readonly token: string;
  /** Fresh P-256 key the credential is bound to (`cnf.kid`). */
  readonly key: P256Keypair;
  /** Epoch ms. */
  readonly expiresAt: number;
}

/**
 * Host-provided: obtains a delegation token for a space from some user's PDS
 * (com.atproto.space.getDelegationToken). The host chooses which OAuth session to
 * use, e.g. the space authority first, then other members.
 */
export class DelegationSource extends Context.Service<
  DelegationSource,
  {
    readonly issue: (space: string) => Effect.Effect<string, CredentialError>;
    /** Client attestation JWT for spaces that gate on app identity. */
    readonly attestation?: (space: string, aud: string) => Effect.Effect<string, CredentialError>;
  }
>()("internal/spaceSync/DelegationSource") {}

const REASONS: Record<string, CredentialError["reason"]> = {
  SpaceDeleted: "SpaceDeleted",
  SpaceNotFound: "SpaceNotFound",
  UserNotAuthorized: "NotAuthorized",
  AppNotAuthorized: "NotAuthorized",
  NotAuthorized: "NotAuthorized",
  InvalidClientAttestation: "NotAuthorized",
  InvalidDelegationToken: "NoDelegation",
};

const credentialFailure = (space: string, status: number, body: unknown): CredentialError => {
  const code = Predicate.hasProperty(body, "error") && Predicate.isString(body.error) ? body.error : undefined;
  const message = Predicate.hasProperty(body, "message") && Predicate.isString(body.message) ? body.message : `HTTP ${status}`;
  const reason = (code === undefined ? undefined : REASONS[code]) ?? (status >= 500 ? "Transport" : "NotAuthorized");
  return new CredentialError({ space, reason, message: code ? `${code}: ${message}` : message });
};

export class Credentials extends Context.Service<
  Credentials,
  {
    readonly get: (space: string) => Effect.Effect<SpaceCredential, CredentialError>;
    readonly invalidate: (space: string) => Effect.Effect<void>;
  }
>()("internal/spaceSync/Credentials") {
  static readonly layer = Layer.effect(
    Credentials,
    Effect.gen(function* () {
      const config = yield* SpaceSyncConfig;
      const identity = yield* Identity;
      const delegation = yield* DelegationSource;
      const refreshLeadMs = Duration.toMillis(config.credentialRefreshLead);

      const mint = Effect.fn("Credentials.mint")(function* (space: string) {
        const { authority } = yield* parseSpaceRef(space).pipe(
          Effect.mapError(() => new CredentialError({ space, reason: "SpaceNotFound", message: "invalid space ref" })),
        );
        const host = yield* identity.resolve(authority).pipe(
          Effect.mapError((e) => new CredentialError({ space, reason: "Transport", message: e.message })),
        );
        const token = yield* delegation.issue(space);
        const attestation = delegation.attestation
          ? yield* delegation.attestation(space, spaceHostAud(authority))
          : undefined;
        const response = yield* Effect.tryPromise({
          try: async (signal) => {
            // A new keypair per credential, per the proposal.
            const key = await P256Keypair.create();
            const headers = await createSpaceSigHeaders(key, { authorization: `Bearer ${token}` });
            const res = await fetch(new URL("/xrpc/com.atproto.space.getSpaceCredential", host.spaceHost), {
              method: "POST",
              redirect: "error",
              signal,
              headers: { ...headers, "content-type": "application/json", accept: "application/json" },
              body: JSON.stringify({ space, ...(attestation ? { clientAttestation: attestation } : {}) }),
            });
            return { key, status: res.status, body: (await res.json().catch(() => undefined)) as unknown };
          },
          catch: (error) => new CredentialError({ space, reason: "Transport", message: errorMessage(error) }),
        });
        if (response.status !== 200) return yield* credentialFailure(space, response.status, response.body);
        const { credential } = yield* Schema.decodeUnknownEffect(GetSpaceCredentialOutput)(response.body).pipe(
          Effect.mapError((e) => new CredentialError({ space, reason: "Transport", message: e.message })),
        );
        const exp = yield* Effect.try({
          try: () => parseSpaceToken("credential", credential).payload.exp,
          catch: (error) => new CredentialError({ space, reason: "Transport", message: errorMessage(error) }),
        });
        const now = yield* Clock.currentTimeMillis;
        const value: SpaceCredential = { space, token: credential, key: response.key, expiresAt: exp * 1000 };
        return { value, ttlMs: Math.max(0, value.expiresAt - now - refreshLeadMs) };
      });

      const cache = yield* Cache.makeWith(mint, {
        capacity: config.credentialCacheCapacity,
        timeToLive: (exit) => (Exit.isSuccess(exit) ? Duration.millis(exit.value.ttlMs) : Duration.zero),
      });

      return Credentials.of({
        get: (space) => Cache.get(cache, space).pipe(Effect.map((entry) => entry.value)),
        invalidate: (space) => Cache.invalidate(cache, space),
      });
    }),
  );
}