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
import {
  Context,
  Effect,
  Layer,
  Option,
  Schedule,
  type Scope,
  Stream,
} from "effect";
import {
  ACCEPT_SIGNATURE_HEADER,
  COMMIT_MAC_PROFILE,
  INVALID_SPACE_MAC,
  type MacProfile,
  encapsulate,
  formatMacRequest,
  type Encapsulation,
} from "../spaceMac";
import { SpaceSyncConfig } from "./config";
import { Credentials } from "./Credentials";
import { CredentialError, XrpcError, errorMessage } from "./errors";
import { Identity, type ResolvedIdentity, requirePds } from "./Identity";
import { failureStatus, signedAgent } from "./wire";

export type SpaceCallError = XrpcError | CredentialError;

interface Target {
  readonly space: SpaceRef;
  readonly method: Query | Procedure;
  /** Repo DID for repo calls, authority DID for space-host calls. */
  readonly audience: DidString;
  /**
   * Both resolve to the audience's PDS; "space" marks a call answered by the
   * authority's space host, the only host that speaks for the space.
   */
  readonly hostOf: "repo" | "space";
  /**
   * Set on a MAC-bearing repo call: the request carries an Accept-Signature
   * header with a key share and the response's MAC is keyed by the shared
   * secret it negotiates.
   */
  readonly macProfile?: MacProfile;
}

/**
 * What a MAC-bearing response is authenticated by: the key share and shared
 * secret the request negotiated, and the response's headers (the covered
 * Atproto-Space-Commit header, Signature-Input and Signature). Verification
 * is the caller's job.
 */
export interface ResponseMac {
  readonly keyShare: string;
  readonly sharedSecret: Uint8Array;
  readonly headers: Headers;
}

/**
 * The did:web a host serving at `endpoint` publishes its own keys under.
 * did:web encodes a port's colon as %3A.
 */
export const hostDid = (endpoint: string): DidString =>
  `did:web:${encodeURIComponent(new URL(endpoint).host)}` as DidString;

const sameHost = (a: string | undefined, b: string): boolean => {
  if (!a) return false;
  try {
    return new URL(a).host.toLowerCase() === new URL(b).host.toLowerCase();
  } catch {
    return false;
  }
};

const isTransient = (error: SpaceCallError): boolean =>
  error._tag === "XrpcError" ? error.transient : error.reason === "Transport";

export class SpaceClient extends Context.Service<
  SpaceClient,
  {
    readonly listWriters: (
      space: SpaceRef,
      cursor?: string,
    ) => Effect.Effect<
      com.atproto.space.listWriters.$OutputBody,
      SpaceCallError
    >;
    readonly listRepoOps: (
      space: SpaceRef,
      repo: DidString,
      since: string | undefined,
      cursor?: string,
    ) => Effect.Effect<
      {
        readonly body: com.atproto.space.listRepoOps.$OutputBody;
        /** Authenticates `body.commit`; set whenever the page has one. */
        readonly mac: ResponseMac | undefined;
      },
      SpaceCallError
    >;
    readonly getRepo: (
      space: SpaceRef,
      repo: DidString,
    ) => Effect.Effect<
      {
        readonly star: AsyncIterable<Uint8Array>;
        /** Authenticates the archive's commit. */
        readonly mac: ResponseMac;
      },
      SpaceCallError,
      Scope.Scope
    >;
    /**
     * Re-resolve the KEM key a repo's MACs are keyed by, after one failed to
     * verify (the key may have rotated). True if the key changed, so a retry
     * can succeed.
     */
    readonly refreshKem: (
      space: SpaceRef,
      repo: DidString,
    ) => Effect.Effect<boolean, SpaceCallError>;
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

      const space = com.atproto.space;

      const identityError = (
        target: Target,
        message: string,
        transient = true,
      ) =>
        new XrpcError({
          method: target.method.nsid,
          status: 0,
          error: "IdentityError",
          message,
          transient,
        });

      /**
       * The KEM key a request about `resolved`'s repo at `host` is keyed by:
       * the author's `#atproto_kem` when the host is their PDS, else the
       * host's own, published in its did:web document. Habitat hosts space
       * repos for authors whose identity lives on a PDS it does not control,
       * and so whose DID document it cannot publish a key in; it keys their
       * MACs with its own KEM key.
       */
      const kemKeyFor = (
        target: Target,
        resolved: ResolvedIdentity,
        host: string,
        lookup: (did: DidString) => Effect.Effect<ResolvedIdentity, unknown>,
      ): Effect.Effect<string, XrpcError> =>
        Effect.gen(function* () {
          const owner = sameHost(resolved.pds, host)
            ? resolved
            : yield* lookup(hostDid(host)).pipe(
                Effect.mapError((e) =>
                  identityError(target, `host kem key: ${errorMessage(e)}`),
                ),
              );
          if (!owner.kemKey)
            return yield* identityError(
              target,
              `${owner.did} publishes no #atproto_kem key`,
              false,
            );
          return owner.kemKey;
        });

      /**
       * The host a target's request goes to. Repo hosts and space hosts are
       * both the audience's PDS: a space authority resolves like any other
       * account.
       */
      const hostFor = (target: Target, resolved: ResolvedIdentity) =>
        requirePds(resolved).pipe(
          Effect.mapError((e) => identityError(target, e.message)),
        );

      /**
       * One signed request to the target's host. `run` throws an xrpc failure
       * (as `xrpc` and `XrpcResponse.fromFetchResponse` do) on an error response.
       * For a MAC-bearing target it is handed the Accept-Signature header to send,
       * and the request's key share and shared secret are returned beside its
       * result.
       */
      const attempt = <A>(
        target: Target,
        run: (
          agent: Agent,
          signal: AbortSignal,
          headers: Record<string, string>,
        ) => Promise<A>,
      ): Effect.Effect<
        {
          readonly result: A;
          readonly encapsulation: Encapsulation | undefined;
        },
        SpaceCallError
      > =>
        Effect.gen(function* () {
          const resolved = yield* identity
            .resolve(target.audience)
            .pipe(Effect.mapError((e) => identityError(target, e.message)));
          const host = yield* hostFor(target, resolved);
          // A fresh key share for every request, retries included.
          let encapsulation: Encapsulation | undefined;
          const headers: Record<string, string> = {};
          if (target.macProfile) {
            const kemKey = yield* kemKeyFor(
              target,
              resolved,
              host,
              identity.resolve,
            );
            encapsulation = yield* Effect.tryPromise({
              try: () => encapsulate(kemKey),
              catch: (e) =>
                identityError(
                  target,
                  `invalid #atproto_kem key: ${errorMessage(e)}`,
                  false,
                ),
            });
            headers[ACCEPT_SIGNATURE_HEADER] = formatMacRequest(
              target.macProfile,
              encapsulation.keyShare,
            );
          }
          const credential = yield* credentials.get(target.space);
          const agent = signedAgent(host, () =>
            createSpaceSigHeaders(credential.key, {
              authorization: `Atproto-Space-Credential ${credential.token}`,
              audience: target.audience,
            }),
          );
          const result = yield* Effect.tryPromise({
            try: (signal) => run(agent, signal, headers),
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
          return { result, encapsulation };
        });

      /**
       * Re-resolve the repo's identity and its host's, so the next request
       * sees a rotated KEM key. True if the KEM key changed.
       */
      const refreshKem = Effect.fnUntraced(function* (
        ref: SpaceRef,
        repo: DidString,
      ) {
        const target: Target = {
          space: ref,
          method: space.listRepoOps.main,
          audience: repo,
          hostOf: "repo",
          macProfile: COMMIT_MAC_PROFILE,
        };
        const keyOf = (
          lookup: (did: DidString) => Effect.Effect<ResolvedIdentity, unknown>,
        ) =>
          lookup(repo).pipe(
            Effect.mapError((e) => identityError(target, errorMessage(e))),
            Effect.flatMap((resolved) =>
              hostFor(target, resolved).pipe(
                Effect.flatMap((host) =>
                  kemKeyFor(target, resolved, host, lookup),
                ),
              ),
            ),
            Effect.option,
          );
        const before = yield* keyOf(identity.resolve);
        const after = yield* keyOf(identity.refresh);
        return (
          Option.isSome(after) &&
          (Option.isNone(before) || before.value !== after.value)
        );
      });

      const call = <A>(
        target: Target,
        run: (
          agent: Agent,
          signal: AbortSignal,
          headers: Record<string, string>,
        ) => Promise<A>,
      ) =>
        attempt(target, run).pipe(
          Effect.catchTag("XrpcError", (e) => {
            // A rejected credential is re-minted and the call retried once.
            if (e.error === "JwtExpired" || e.error === "CredentialRevoked")
              return credentials
                .invalidate(target.space)
                .pipe(Effect.flatMap(() => attempt(target, run)));
            // A refused key share may follow a KEM key rotation: re-resolve
            // and retry once if the key changed.
            if (e.error === INVALID_SPACE_MAC && target.macProfile)
              return refreshKem(target.space, target.audience).pipe(
                Effect.flatMap((changed) =>
                  changed ? attempt(target, run) : Effect.fail(e),
                ),
              );
            return Effect.fail(e);
          }),
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

      const listWriters = Effect.fnUntraced(function* (
        ref: SpaceRef,
        cursor?: string,
      ) {
        const method = space.listWriters.main;
        const res = yield* call(
          { space: ref, method, audience: ref.spaceDid, hostOf: "space" },
          (agent, signal) =>
            xrpc(agent, method, {
              params: { space: ref.toString(), limit: 1000, cursor },
              signal,
            }),
        );
        return res.result.body;
      });

      const listRepoOps = Effect.fnUntraced(function* (
        ref: SpaceRef,
        repo: DidString,
        since: string | undefined,
        cursor?: string,
      ) {
        const method = space.listRepoOps.main;
        const { result, encapsulation } = yield* call(
          {
            space: ref,
            method,
            audience: repo,
            hostOf: "repo",
            macProfile: COMMIT_MAC_PROFILE,
          },
          (agent, signal, headers) =>
            xrpc(agent, method, {
              params: {
                space: ref.toString(),
                repo,
                since: since as TidString | undefined,
                cursor,
                limit: 1000,
              },
              headers,
              signal,
            }),
        );
        // Only a page carrying the commit carries a MAC.
        const mac =
          result.body.commit && encapsulation
            ? { ...encapsulation, headers: result.headers }
            : undefined;
        return { body: result.body, mac };
      });

      /**
       * The STAR body as a stream that fails if the host goes quiet for
       * `streamIdleTimeout` between chunks. When iteration ends or is
       * abandoned the stream only releases its reader; getRepo's scope cancels
       * the body.
       */
      const starStream = (body: ReadableStream<Uint8Array>) =>
        Stream.fromReadableStream({
          evaluate: () => body,
          releaseLockOnEnd: true,
          onError: (error) =>
            new XrpcError({
              method: space.getRepo.main.nsid,
              status: 0,
              error: "Transport",
              message: errorMessage(error),
              transient: true,
            }),
        }).pipe(
          Stream.timeoutOrElse({
            duration: config.streamIdleTimeout,
            orElse: () =>
              Stream.fail(
                new XrpcError({
                  method: space.getRepo.main.nsid,
                  status: 0,
                  error: "Timeout",
                  message: "STAR stream stalled",
                  transient: true,
                }),
              ),
          }),
        );

      // getRepo bypasses `xrpc`, which buffers the whole body, so the archive can
      // be verified as it streams.
      const getRepo = (ref: SpaceRef, repo: DidString) => {
        const method = space.getRepo.main;
        return Effect.acquireRelease(
          call(
            {
              space: ref,
              method,
              audience: repo,
              hostOf: "repo",
              macProfile: COMMIT_MAC_PROFILE,
            },
            async (agent, signal, macHeaders) => {
              const params = method.parameters.toURLSearchParams({
                space: ref.toString(),
                repo,
              });
              const res = await agent.fetchHandler(
                `/xrpc/${method.nsid}?${params}`,
                {
                  method: "GET",
                  signal,
                  headers: { ...macHeaders, accept: method.output.encoding },
                },
              );
              // Throws the parsed xrpc error for 4xx/5xx responses.
              if (!res.ok) await XrpcResponse.fromFetchResponse(method, res);
              return res;
            },
          ),
          // Cancels an unfinished download when the scope closes (e.g. on
          // interruption). Deliberately not awaited: a cancel can wait on the
          // other side indefinitely (a tee()'d body only settles once every
          // branch is cancelled), and closing the scope must not hang on it.
          ({ result: res }) =>
            Effect.sync(() => void res.body?.cancel().catch(() => undefined)),
        ).pipe(
          Effect.flatMap(({ result: res, encapsulation }) =>
            res.body && encapsulation
              ? Effect.succeed({
                  star: Stream.toAsyncIterable(starStream(res.body)),
                  mac: { ...encapsulation, headers: res.headers },
                })
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
        const bytes = res.result.body;
        // The repo host is untrusted transport: the blob must hash to its CID.
        const matches = yield* Effect.tryPromise({
          try: () => isCidForBytes(expected, bytes),
          catch: (error) =>
            invalidResponse(method, res.result.status, errorMessage(error)),
        });
        if (!matches)
          return yield* invalidResponse(
            method,
            res.result.status,
            "blob does not match its cid",
          );
        return bytes;
      });

      return SpaceClient.of({
        listWriters,
        listRepoOps,
        getRepo,
        getBlob,
        refreshKem,
      });
    }),
  );
}
