import type { SpaceRef, SpaceRefString } from "@atproto/syntax";
import { P256Keypair } from "@atproto/crypto";
import { type XrpcFailure, XrpcResponseError, xrpcSafe } from "@atproto/lex";
import { com } from "api";
import {
  createSpaceSigHeaders,
  parseSpaceToken,
  spaceHostAud,
} from "@atproto/space";
import {
  Cache,
  Clock,
  Context,
  Duration,
  Effect,
  Equal,
  Exit,
  Hash,
  Layer,
} from "effect";
import { SpaceSyncConfig } from "./config";
import { CredentialError, errorMessage } from "./errors";
import { Identity } from "./Identity";
import { signedAgent } from "./wire";

export interface SpaceCredential {
  readonly space: SpaceRefString;
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
    readonly issue: (
      space: SpaceRefString,
    ) => Effect.Effect<string, CredentialError>;
    /** Client attestation JWT for spaces that gate on app identity. */
    readonly attestation?: (
      space: SpaceRefString,
      aud: string,
    ) => Effect.Effect<string, CredentialError>;
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

const credentialFailure = (
  space: SpaceRefString,
  failure: XrpcFailure,
): CredentialError => {
  // Only an error response from the authority says anything about access;
  // network failures and malformed responses are transient.
  if (!(failure instanceof XrpcResponseError))
    return new CredentialError({
      space,
      reason: "Transport",
      message: failure.message,
    });
  const reason =
    REASONS[failure.error] ??
    (failure.status >= 500 ? "Transport" : "NotAuthorized");
  return new CredentialError({
    space,
    reason,
    message: `${failure.error}: ${failure.message}`,
  });
};

/** Cache key that compares by value, so every lookup for a space shares one credential. */
class SpaceKey implements Equal.Equal {
  readonly id: SpaceRefString;
  constructor(readonly ref: SpaceRef) {
    this.id = ref.toString();
  }
  [Equal.symbol](that: unknown): boolean {
    return that instanceof SpaceKey && that.id === this.id;
  }
  [Hash.symbol](): number {
    return Hash.string(this.id);
  }
}

export class Credentials extends Context.Service<
  Credentials,
  {
    readonly get: (
      space: SpaceRef,
    ) => Effect.Effect<SpaceCredential, CredentialError>;
    readonly invalidate: (space: SpaceRef) => Effect.Effect<void>;
  }
>()("internal/spaceSync/Credentials") {
  static readonly layer = Layer.effect(
    Credentials,
    Effect.gen(function* () {
      const config = yield* SpaceSyncConfig;
      const identity = yield* Identity;
      const delegation = yield* DelegationSource;
      const refreshLeadMs = Duration.toMillis(config.credentialRefreshLead);

      const mint = Effect.fn("Credentials.mint")(function* ({
        ref,
        id: space,
      }: SpaceKey) {
        const authority = ref.spaceDid;
        const host = yield* identity.resolve(authority).pipe(
          Effect.mapError(
            (e) =>
              new CredentialError({
                space,
                reason: "Transport",
                message: e.message,
              }),
          ),
        );
        const token = yield* delegation.issue(space);
        const attestation = delegation.attestation
          ? yield* delegation.attestation(space, spaceHostAud(authority))
          : undefined;
        // A new keypair per credential, per the proposal.
        const key = yield* Effect.promise(() => P256Keypair.create());
        const agent = signedAgent(host.spaceHost, () =>
          createSpaceSigHeaders(key, { authorization: `Bearer ${token}` }),
        );
        const result = yield* Effect.tryPromise({
          try: (signal) =>
            xrpcSafe(agent, com.atproto.space.getSpaceCredential.main, {
              body: {
                space,
                ...(attestation ? { clientAttestation: attestation } : {}),
              },
              signal,
            }),
          catch: (error) =>
            new CredentialError({
              space,
              reason: "Transport",
              message: errorMessage(error),
            }),
        });
        if (!result.success) return yield* credentialFailure(space, result);
        const { credential } = result.body;
        const exp = yield* Effect.try({
          try: () => parseSpaceToken("credential", credential).payload.exp,
          catch: (error) =>
            new CredentialError({
              space,
              reason: "Transport",
              message: errorMessage(error),
            }),
        });
        const now = yield* Clock.currentTimeMillis;
        const value: SpaceCredential = {
          space,
          token: credential,
          key,
          expiresAt: exp * 1000,
        };
        return {
          value,
          ttlMs: Math.max(0, value.expiresAt - now - refreshLeadMs),
        };
      });

      const cache = yield* Cache.makeWith(mint, {
        capacity: config.credentialCacheCapacity,
        timeToLive: (exit) =>
          Exit.isSuccess(exit)
            ? Duration.millis(exit.value.ttlMs)
            : Duration.zero,
      });

      return Credentials.of({
        get: (space) =>
          Cache.get(cache, new SpaceKey(space)).pipe(
            Effect.map((entry) => entry.value),
          ),
        invalidate: (space) => Cache.invalidate(cache, new SpaceKey(space)),
      });
    }),
  );
}
