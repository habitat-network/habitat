import {
  type Agent,
  type CidString,
  type Procedure,
  type Query,
  type TidString,
  XrpcResponse,
  asXrpcFailure,
  isCidForBytes,
  parseCidSafe,
  xrpc,
} from "@atproto/lex";
import type { DidString, SpaceRef } from "@atproto/syntax";
import { createSpaceSigHeaders } from "@atproto/space";
import { com } from "api";
import { Context, Duration, Effect, Layer, Schedule, type Scope } from "effect";
import { SpaceSyncConfig } from "./config";
import { Credentials } from "./Credentials";
import { CredentialError, XrpcError, errorMessage } from "./errors";
import { Identity } from "./Identity";
import { failureStatus, signedAgent } from "./wire";

export type SpaceCallError = XrpcError | CredentialError;

interface Target {
  readonly space: SpaceRef;
  readonly method: Query | Procedure;
  /** Repo DID for repo calls, authority DID for space-host calls. */
  readonly audience: DidString;
  /** "repo" → audience's PDS; "space" → audience's space host. */
  readonly hostOf: "repo" | "space";
}

const isTransient = (error: SpaceCallError): boolean =>
  error._tag === "XrpcError" ? error.transient : error.reason === "Transport";

/**
 * A fetch body's own async iterator deadlocks when a consumer abandons it
 * mid-stream: `return()` queues behind an in-flight `next()` that the stalled
 * reader never settles. Carve out our own iterator over a locked reader so
 * disposal just cancels the body. A host that goes quiet for `idleMs` fails
 * the stream rather than holding the pass forever.
 */
async function* carChunks(
  body: ReadableStream<Uint8Array>,
  idleMs: number,
): AsyncIterable<Uint8Array> {
  const reader = body.getReader();
  try {
    while (true) {
      let timer: ReturnType<typeof setTimeout> | undefined;
      const idle = new Promise<never>((_, reject) => {
        timer = setTimeout(
          () => reject(new Error(`CAR stream stalled for ${idleMs}ms`)),
          idleMs,
        );
      });
      const { done, value } = await Promise.race([reader.read(), idle]).finally(
        () => clearTimeout(timer),
      );
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
      space: SpaceRef,
      cursor?: string,
    ) => Effect.Effect<com.atproto.space.listRepos.$OutputBody, SpaceCallError>;
    readonly registerNotify: (
      space: SpaceRef,
      service: string,
    ) => Effect.Effect<{ readonly expiresAt: number }, SpaceCallError>;
    readonly listRepoOps: (
      space: SpaceRef,
      repo: DidString,
      since: string | undefined,
      cursor?: string,
    ) => Effect.Effect<
      com.atproto.space.listRepoOps.$OutputBody,
      SpaceCallError
    >;
    readonly getRepo: (
      space: SpaceRef,
      repo: DidString,
    ) => Effect.Effect<AsyncIterable<Uint8Array>, SpaceCallError, Scope.Scope>;
    /** Fetches a blob and checks its bytes against `cid`. */
    readonly getBlob: (
      space: SpaceRef,
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
      const idleMs = Duration.toMillis(config.streamIdleTimeout);

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
                  transient: true,
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
              // Only the authority's space host speaks for the space. A repo
              // host answering SpaceDeleted must not wipe everyone's data.
              return failure.error === "SpaceDeleted" &&
                target.hostOf === "space"
                ? new CredentialError({
                    space: target.space.toString(),
                    reason: "SpaceDeleted",
                    message: failure.message,
                  })
                : new XrpcError({
                    method: target.method.nsid,
                    status: failureStatus(failure),
                    error: failure.error,
                    message: failure.message,
                    transient: failure.shouldRetry(),
                  });
            },
          }).pipe(
            // Interrupting the request aborts its fetch through `signal`.
            Effect.timeoutOrElse({
              duration: config.requestTimeout,
              orElse: () =>
                Effect.fail(
                  new XrpcError({
                    method: target.method.nsid,
                    status: 0,
                    error: "Timeout",
                    message: "no response before the request timeout",
                    transient: true,
                  }),
                ),
            }),
          );
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

      const invalidResponse = (
        method: Query | Procedure,
        status: number,
        message: string,
      ) =>
        new XrpcError({
          method: method.nsid,
          status,
          error: "InvalidResponse",
          message,
          transient: false,
        });

      const listRepos = Effect.fnUntraced(function* (
        ref: SpaceRef,
        cursor?: string,
      ) {
        const method = space.listRepos.main;
        const res = yield* call(
          { space: ref, method, audience: ref.spaceDid, hostOf: "space" },
          (agent, signal) =>
            xrpc(agent, method, {
              params: { space: ref.toString(), limit: 1000, cursor },
              signal,
            }),
        );
        return res.body;
      });

      const registerNotify = Effect.fnUntraced(function* (
        ref: SpaceRef,
        service: string,
      ) {
        const method = space.registerNotify.main;
        const res = yield* call(
          { space: ref, method, audience: ref.spaceDid, hostOf: "space" },
          (agent, signal) =>
            xrpc(agent, method, {
              body: { space: ref.toString(), service },
              signal,
            }),
        );
        const expiresAt = Date.parse(res.body.expiresAt);
        if (!Number.isFinite(expiresAt))
          return yield* invalidResponse(method, 200, "unparseable expiresAt");
        return { expiresAt };
      });

      const listRepoOps = Effect.fnUntraced(function* (
        ref: SpaceRef,
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
                space: ref.toString(),
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
      const getRepo = (ref: SpaceRef, repo: DidString) => {
        const method = space.getRepo.main;
        return Effect.acquireRelease(
          call(
            { space: ref, method, audience: repo, hostOf: "repo" },
            async (agent, signal) => {
              const params = method.parameters.toURLSearchParams({
                space: ref.toString(),
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
              ? Effect.succeed(carChunks(res.body, idleMs))
              : Effect.fail(invalidResponse(method, res.status, "empty body")),
          ),
        );
      };

      const getBlob = Effect.fnUntraced(function* (
        ref: SpaceRef,
        repo: DidString,
        cid: string,
      ) {
        const method = space.getBlob.main;
        const expected = parseCidSafe(cid);
        if (!expected)
          return yield* new XrpcError({
            method: method.nsid,
            status: 0,
            error: "InvalidRequest",
            message: `invalid cid ${cid}`,
            transient: false,
          });
        const res = yield* call(
          { space: ref, method, audience: repo, hostOf: "repo" },
          (agent, signal) =>
            xrpc(agent, method, {
              params: { space: ref.toString(), repo, cid: cid as CidString },
              signal,
            }),
        );
        const bytes = res.body;
        // The repo host is untrusted transport: the blob must hash to its CID.
        const matches = yield* Effect.tryPromise({
          try: () => isCidForBytes(expected, bytes),
          catch: (error) =>
            invalidResponse(method, res.status, errorMessage(error)),
        });
        if (!matches)
          return yield* invalidResponse(
            method,
            res.status,
            "blob does not match its cid",
          );
        return bytes;
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
