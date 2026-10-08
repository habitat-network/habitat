import {
  type Agent,
  type CidString,
  type Procedure,
  type Query,
  type TidString,
  XrpcResponse,
  asXrpcFailure,
  xrpc,
} from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import { createSpaceSigHeaders } from "@atproto/space";
import { com } from "api";
import { Context, Effect, Layer, Schedule, type Scope } from "effect";
import { SpaceSyncConfig } from "./config";
import { Credentials } from "./Credentials";
import { CredentialError, XrpcError } from "./errors";
import { Identity } from "./Identity";
import { failureStatus, parseSpaceRef, signedAgent } from "./wire";

export type SpaceCallError = XrpcError | CredentialError;

interface Target {
  readonly space: SpaceRefString;
  readonly method: Query | Procedure;
  /** Repo DID for repo calls, authority DID for space-host calls. */
  readonly audience: DidString;
  /** "repo" → audience's PDS; "space" → audience's space host. */
  readonly hostOf: "repo" | "space";
}

const isTransient = (error: SpaceCallError): boolean =>
  error._tag === "XrpcError"
    ? error.status === 0 || error.status >= 500
    : error.reason === "Transport";

/**
 * A fetch body's own async iterator deadlocks when a consumer abandons it
 * mid-stream: `return()` queues behind an in-flight `next()` that the stalled
 * reader never settles. Carve out our own iterator over a locked reader so
 * disposal just cancels the body.
 */
async function* carChunks(
  body: ReadableStream<Uint8Array>,
): AsyncIterable<Uint8Array> {
  const reader = body.getReader();
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) return;
      if (value !== undefined) yield value;
    }
  } finally {
    // Deliberately not awaited: a pending read() may never settle.
    void reader.cancel().catch(() => undefined);
  }
}

export class SpaceClient extends Context.Service<
  SpaceClient,
  {
    readonly listRepos: (
      space: SpaceRefString,
      cursor?: string,
    ) => Effect.Effect<com.atproto.space.listRepos.$OutputBody, SpaceCallError>;
    readonly registerNotify: (
      space: SpaceRefString,
      service: string,
    ) => Effect.Effect<{ readonly expiresAt: number }, SpaceCallError>;
    readonly listRepoOps: (
      space: SpaceRefString,
      repo: DidString,
      since: string | undefined,
      cursor?: string,
    ) => Effect.Effect<
      com.atproto.space.listRepoOps.$OutputBody,
      SpaceCallError
    >;
    readonly getRepo: (
      space: SpaceRefString,
      repo: DidString,
    ) => Effect.Effect<AsyncIterable<Uint8Array>, SpaceCallError, Scope.Scope>;
    readonly getBlob: (
      space: SpaceRefString,
      repo: DidString,
      cid: string,
    ) => Effect.Effect<Uint8Array, SpaceCallError>;
  }
>()("internal/spaceSync/SpaceClient") {
  static readonly layer = Layer.effect(
    SpaceClient,
    Effect.gen(function* () {
      const config = yield* SpaceSyncConfig;
      const credentials = yield* Credentials;
      const identity = yield* Identity;

      const space = com.atproto.space;

      /**
       * One signed request to the target's host. `run` throws an xrpc failure
       * (as `xrpc` and `XrpcResponse.fromFetchResponse` do) on an error response.
       */
      const attempt = <A>(
        target: Target,
        run: (agent: Agent, signal: AbortSignal) => Promise<A>,
      ): Effect.Effect<A, SpaceCallError> =>
        Effect.gen(function* () {
          const resolved = yield* identity.resolve(target.audience).pipe(
            Effect.mapError(
              (e) =>
                new XrpcError({
                  method: target.method.nsid,
                  status: 0,
                  error: "IdentityError",
                  message: e.message,
                }),
            ),
          );
          const credential = yield* credentials.get(target.space);
          const agent = signedAgent(
            target.hostOf === "repo" ? resolved.pds : resolved.spaceHost,
            () =>
              createSpaceSigHeaders(credential.key, {
                authorization: `Atproto-Space ${credential.token}`,
                audience: target.audience,
              }),
          );
          return yield* Effect.tryPromise({
            try: (signal) => run(agent, signal),
            catch: (cause): SpaceCallError => {
              const failure = asXrpcFailure(target.method, cause);
              return failure.error === "SpaceDeleted"
                ? new CredentialError({
                    space: target.space,
                    reason: "SpaceDeleted",
                    message: failure.message,
                  })
                : new XrpcError({
                    method: target.method.nsid,
                    status: failureStatus(failure),
                    error: failure.error,
                    message: failure.message,
                  });
            },
          });
        });

      const call = <A>(
        target: Target,
        run: (agent: Agent, signal: AbortSignal) => Promise<A>,
      ) =>
        attempt(target, run).pipe(
          // A rejected credential is re-minted and the call retried once.
          Effect.catchTag("XrpcError", (e) =>
            e.error === "JwtExpired" || e.error === "CredentialRevoked"
              ? credentials
                  .invalidate(target.space)
                  .pipe(Effect.flatMap(() => attempt(target, run)))
              : Effect.fail(e),
          ),
          Effect.retry({
            schedule: Schedule.exponential(config.requestRetryBase).pipe(
              Schedule.jittered,
            ),
            times: config.requestRetries,
            while: isTransient,
          }),
        );

      const authorityOf = (ref: SpaceRefString) =>
        parseSpaceRef(ref).pipe(
          Effect.map((parsed) => parsed.authority),
          Effect.mapError(
            () =>
              new CredentialError({
                space: ref,
                reason: "SpaceNotFound",
                message: "invalid space ref",
              }),
          ),
        );

      const listRepos = Effect.fnUntraced(function* (
        ref: SpaceRefString,
        cursor?: string,
      ) {
        const method = space.listRepos.main;
        const target: Target = {
          space: ref,
          method,
          audience: yield* authorityOf(ref),
          hostOf: "space",
        };
        const res = yield* call(target, (agent, signal) =>
          xrpc(agent, method, {
            params: { space: ref, limit: 1000, cursor },
            signal,
          }),
        );
        return res.body;
      });

      const registerNotify = Effect.fnUntraced(function* (
        ref: SpaceRefString,
        service: string,
      ) {
        const method = space.registerNotify.main;
        const target: Target = {
          space: ref,
          method,
          audience: yield* authorityOf(ref),
          hostOf: "space",
        };
        const res = yield* call(target, (agent, signal) =>
          xrpc(agent, method, { body: { space: ref, service }, signal }),
        );
        const expiresAt = Date.parse(res.body.expiresAt);
        if (!Number.isFinite(expiresAt)) {
          return yield* new XrpcError({
            method: method.nsid,
            status: 200,
            error: "InvalidResponse",
            message: "unparseable expiresAt",
          });
        }
        return { expiresAt };
      });

      const listRepoOps = Effect.fnUntraced(function* (
        ref: SpaceRefString,
        repo: DidString,
        since: string | undefined,
        cursor?: string,
      ) {
        const method = space.listRepoOps.main;
        const res = yield* call(
          { space: ref, method, audience: repo, hostOf: "repo" },
          (agent, signal) =>
            xrpc(agent, method, {
              params: {
                space: ref,
                repo,
                since: since as TidString | undefined,
                cursor,
                limit: 1000,
              },
              signal,
            }),
        );
        return res.body;
      });

      // getRepo bypasses `xrpc`, which buffers the whole body, so the CAR can
      // be verified as it streams.
      const getRepo = (ref: SpaceRefString, repo: DidString) => {
        const method = space.getRepo.main;
        return Effect.acquireRelease(
          call(
            { space: ref, method, audience: repo, hostOf: "repo" },
            async (agent, signal) => {
              const params = method.parameters.toURLSearchParams({
                space: ref,
                repo,
              });
              const res = await agent.fetchHandler(
                `/xrpc/${method.nsid}?${params}`,
                {
                  method: "GET",
                  signal,
                  headers: { accept: method.output.encoding },
                },
              );
              // Throws the parsed xrpc error for 4xx/5xx responses.
              if (!res.ok) await XrpcResponse.fromFetchResponse(method, res);
              return res;
            },
          ),
          // Cancels an unfinished download when the scope closes (e.g. on interruption).
          (res) =>
            Effect.promise(
              () =>
                res.body?.cancel().catch(() => undefined) ?? Promise.resolve(),
            ),
        ).pipe(
          Effect.flatMap((res) =>
            res.body
              ? Effect.succeed(carChunks(res.body))
              : Effect.fail(
                  new XrpcError({
                    method: method.nsid,
                    status: res.status,
                    error: "InvalidResponse",
                    message: "empty body",
                  }),
                ),
          ),
        );
      };

      const getBlob = Effect.fnUntraced(function* (
        ref: SpaceRefString,
        repo: DidString,
        cid: string,
      ) {
        const method = space.getBlob.main;
        const res = yield* call(
          { space: ref, method, audience: repo, hostOf: "repo" },
          (agent, signal) =>
            xrpc(agent, method, {
              params: { space: ref, repo, cid: cid as CidString },
              signal,
            }),
        );
        return res.body;
      });

      return SpaceClient.of({
        listRepos,
        registerNotify,
        listRepoOps,
        getRepo,
        getBlob,
      });
    }),
  );
}
