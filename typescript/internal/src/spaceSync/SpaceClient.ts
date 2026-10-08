import type { DidString } from "@atproto/syntax";
import { createSpaceSigHeaders } from "@atproto/space";
import {
  Context,
  Effect,
  Layer,
  Predicate,
  Schedule,
  Schema,
  type Scope,
} from "effect";
import { SpaceSyncConfig } from "./config";
import { Credentials } from "./Credentials";
import { CredentialError, XrpcError, errorMessage } from "./errors";
import { Identity } from "./Identity";
import {
  ListRepoOpsOutput,
  ListReposOutput,
  RegisterNotifyOutput,
  invalidResponse,
  lexJson,
  parseSpaceRef,
} from "./wire";

export type SpaceCallError = XrpcError | CredentialError;

interface CallInput {
  readonly space: string;
  readonly method: string;
  /** Repo DID for repo calls, authority DID for space-host calls. */
  readonly audience: string;
  /** "repo" → audience's PDS; "space" → audience's space host. */
  readonly hostOf: "repo" | "space";
  readonly params?: Record<string, string | undefined>;
  readonly body?: unknown;
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
      space: string,
      cursor?: string,
    ) => Effect.Effect<typeof ListReposOutput.Type, SpaceCallError>;
    readonly registerNotify: (
      space: string,
      service: string,
    ) => Effect.Effect<{ readonly expiresAt: number }, SpaceCallError>;
    readonly listRepoOps: (
      space: string,
      repo: string,
      since: string | undefined,
      cursor?: string,
    ) => Effect.Effect<typeof ListRepoOpsOutput.Type, SpaceCallError>;
    readonly getRepo: (
      space: string,
      repo: string,
    ) => Effect.Effect<AsyncIterable<Uint8Array>, SpaceCallError, Scope.Scope>;
    readonly getBlob: (
      space: string,
      repo: string,
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

      const attempt = (
        input: CallInput,
      ): Effect.Effect<Response, SpaceCallError> =>
        Effect.gen(function* () {
          const resolved = yield* identity
            .resolve(input.audience)
            .pipe(
              Effect.mapError(
                (e) =>
                  new XrpcError({
                    method: input.method,
                    status: 0,
                    error: "IdentityError",
                    message: e.message,
                  }),
              ),
            );
          const credential = yield* credentials.get(input.space);
          const response = yield* Effect.tryPromise({
            try: async (signal) => {
              const url = new URL(
                `/xrpc/${input.method}`,
                input.hostOf === "repo" ? resolved.pds : resolved.spaceHost,
              );
              for (const [name, value] of Object.entries(input.params ?? {})) {
                if (value !== undefined) url.searchParams.set(name, value);
              }
              const headers = await createSpaceSigHeaders(credential.key, {
                authorization: `Atproto-Space ${credential.token}`,
                audience: input.audience as DidString,
              });
              return fetch(url, {
                method: input.body === undefined ? "GET" : "POST",
                redirect: "error",
                signal,
                headers:
                  input.body === undefined
                    ? headers
                    : { ...headers, "content-type": "application/json" },
                body:
                  input.body === undefined
                    ? undefined
                    : JSON.stringify(input.body),
              });
            },
            catch: (error) =>
              new XrpcError({
                method: input.method,
                status: 0,
                error: "Transport",
                message: errorMessage(error),
              }),
          });
          if (response.ok) return response;
          const body: unknown = yield* Effect.promise(() =>
            response.json().catch(() => undefined),
          );
          const error =
            Predicate.hasProperty(body, "error") &&
            Predicate.isString(body.error)
              ? body.error
              : undefined;
          const message =
            Predicate.hasProperty(body, "message") &&
            Predicate.isString(body.message)
              ? body.message
              : `HTTP ${response.status}`;
          if (error === "SpaceDeleted") {
            return yield* new CredentialError({
              space: input.space,
              reason: "SpaceDeleted",
              message,
            });
          }
          return yield* new XrpcError({
            method: input.method,
            status: response.status,
            error,
            message,
          });
        });

      const call = Effect.fnUntraced(function* (input: CallInput) {
        return yield* attempt(input).pipe(
          // A rejected credential is re-minted and the call retried once.
          Effect.catchTag("XrpcError", (e) =>
            e.error === "JwtExpired" || e.error === "CredentialRevoked"
              ? credentials
                  .invalidate(input.space)
                  .pipe(Effect.flatMap(() => attempt(input)))
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
      });

      const readJson = (method: string, response: Response) =>
        Effect.tryPromise({
          try: async () => lexJson(await response.json()),
          catch: (error) =>
            new XrpcError({
              method,
              status: response.status,
              error: "InvalidResponse",
              message: errorMessage(error),
            }),
        });

      const authorityOf = (space: string) =>
        parseSpaceRef(space).pipe(
          Effect.map((ref) => ref.authority),
          Effect.mapError(
            () =>
              new CredentialError({
                space,
                reason: "SpaceNotFound",
                message: "invalid space ref",
              }),
          ),
        );

      const listRepos = Effect.fnUntraced(function* (
        space: string,
        cursor?: string,
      ) {
        const method = "com.atproto.space.listRepos";
        const authority = yield* authorityOf(space);
        const res = yield* call({
          space,
          method,
          audience: authority,
          hostOf: "space",
          params: { space, limit: "1000", cursor },
        });
        return yield* Schema.decodeUnknownEffect(ListReposOutput)(
          yield* readJson(method, res),
        ).pipe(Effect.mapError(invalidResponse(method)));
      });

      const registerNotify = Effect.fnUntraced(function* (
        space: string,
        service: string,
      ) {
        const method = "com.atproto.space.registerNotify";
        const authority = yield* authorityOf(space);
        const res = yield* call({
          space,
          method,
          audience: authority,
          hostOf: "space",
          body: { space, service },
        });
        const out = yield* Schema.decodeUnknownEffect(RegisterNotifyOutput)(
          yield* readJson(method, res),
        ).pipe(Effect.mapError(invalidResponse(method)));
        const expiresAt = Date.parse(out.expiresAt);
        if (!Number.isFinite(expiresAt)) {
          return yield* new XrpcError({
            method,
            status: 200,
            error: "InvalidResponse",
            message: "unparseable expiresAt",
          });
        }
        return { expiresAt };
      });

      const listRepoOps = Effect.fnUntraced(function* (
        space: string,
        repo: string,
        since: string | undefined,
        cursor?: string,
      ) {
        const method = "com.atproto.space.listRepoOps";
        const res = yield* call({
          space,
          method,
          audience: repo,
          hostOf: "repo",
          params: { space, repo, since, cursor, limit: "1000" },
        });
        return yield* Schema.decodeUnknownEffect(ListRepoOpsOutput)(
          yield* readJson(method, res),
        ).pipe(Effect.mapError(invalidResponse(method)));
      });

      const getRepo = (space: string, repo: string) =>
        Effect.acquireRelease(
          call({
            space,
            method: "com.atproto.space.getRepo",
            audience: repo,
            hostOf: "repo",
            params: { space, repo },
          }),
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
                    method: "com.atproto.space.getRepo",
                    status: res.status,
                    error: "InvalidResponse",
                    message: "empty body",
                  }),
                ),
          ),
        );

      const getBlob = Effect.fnUntraced(function* (
        space: string,
        repo: string,
        cid: string,
      ) {
        const method = "com.atproto.space.getBlob";
        const res = yield* call({
          space,
          method,
          audience: repo,
          hostOf: "repo",
          params: { space, repo, cid },
        });
        return yield* Effect.tryPromise({
          try: async () => new Uint8Array(await res.arrayBuffer()),
          catch: (error) =>
            new XrpcError({
              method,
              status: res.status,
              error: "InvalidResponse",
              message: errorMessage(error),
            }),
        });
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
