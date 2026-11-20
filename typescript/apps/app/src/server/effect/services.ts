import { env as workerEnv } from "cloudflare:workers";
import {
  xrpc as lexXrpc,
  XrpcError,
  type Agent,
  type Main,
  type Procedure,
  type Query,
  type XrpcOptions,
  type XrpcResponse,
} from "@atproto/lex";
import type { OAuthClient, OAuthSession } from "@atproto/oauth-client";
import { Context, Effect, Layer, Predicate } from "effect";
import { getDb, type Db } from "@/db";
import { createOAuthClient } from "@/server/oauth";
import {
  HabitatClient,
  UnauthorizedError,
  instanceProxy,
} from "@/server/habitatClient";
import { HabitatError, SessionExpired } from "./errors";

// The services every server-side Effect in the app can ask for. They're all
// built once per isolate from the Worker's bindings (see ./run.ts's runtime);
// anything request-scoped (the session cookie, the signed-in member) is
// provided per call by runEffect instead.

// AppEnv is the Worker's bindings and vars.
export class AppEnv extends Context.Service<AppEnv, Env>()("app/AppEnv") {
  static readonly layer = Layer.succeed(AppEnv, workerEnv);
}

// Database is the app's D1 database through drizzle.
export class Database extends Context.Service<Database, Db>()("app/Database") {
  static readonly layer = Layer.effect(
    Database,
    Effect.gen(function* () {
      return getDb(yield* AppEnv);
    }),
  );
}

// OAuth is the app's atproto OAuth client of the Habitat instance.
export class OAuth extends Context.Service<OAuth, OAuthClient>()("app/OAuth") {
  static readonly layer = Layer.effect(
    OAuth,
    Effect.gen(function* () {
      return createOAuthClient(yield* AppEnv);
    }),
  );
}

// CallOptions tune one call to the Habitat instance.
export interface CallOptions {
  // proxy names the DID whose `#habitat` service should handle the call
  // (sent as Atproto-Proxy). community.opensocial.* and other
  // management-plane methods are served by the group's own Habitat
  // instance, which a member's session doesn't necessarily talk to directly.
  readonly proxy?: string;
}

// Habitat makes calls to the Habitat instance as a specific DID (a member,
// or a group a member has connected), using the OAuth session the app holds
// for it. Every failure is typed: SessionExpired when pear no longer accepts
// the session, HabitatError for anything else.
export class Habitat extends Context.Service<
  Habitat,
  {
    // session restores the DID's OAuth session.
    readonly session: (
      did: string,
    ) => Effect.Effect<OAuthSession, SessionExpired>;
    // xrpc is @atproto/lex's typed xrpc() as the DID. It answers with the
    // response body, already decoded against the method's output schema.
    readonly xrpc: <const M extends Query | Procedure>(
      did: string,
      method: Main<M>,
      options?: XrpcOptions<M>,
      call?: CallOptions,
    ) => Effect.Effect<XrpcResponse<M>["body"], HabitatError | SessionExpired>;
    // call is an untyped call by NSID, for methods whose lexicon the api
    // package doesn't generate a schema for, or whose body the app reads
    // loosely on purpose. GET payloads become query parameters.
    readonly call: <T>(
      did: string,
      nsid: string,
      method: "GET" | "POST",
      payload: Record<string, unknown>,
      call?: CallOptions,
    ) => Effect.Effect<T, HabitatError | SessionExpired>;
    // client is the Promise-based HabitatClient for the same DID, for the
    // code ported from Chalk that isn't written as Effects (the doc rooms,
    // the sync sink, comments).
    readonly client: (did: string) => HabitatClient;
  }
>()("app/Habitat") {
  static readonly layer = Layer.effect(
    Habitat,
    Effect.gen(function* () {
      const env = yield* AppEnv;
      const oauth = yield* OAuth;

      const session = Effect.fn("Habitat.session")(function* (did: string) {
        return yield* Effect.tryPromise({
          try: () => oauth.restore(did),
          catch: (cause) =>
            new SessionExpired({
              did,
              message: `restoring the OAuth session for ${did} failed: ${messageOf(cause)}`,
            }),
        });
      });

      const xrpc = <const M extends Query | Procedure>(
        did: string,
        method: Main<M>,
        options?: XrpcOptions<M>,
        call?: CallOptions,
      ) =>
        Effect.gen(function* () {
          const oauthSession = yield* session(did);
          const agent: Agent = oauthSession;
          const nsid = methodNsid(method);
          const opts = withProxy(
            options ?? {},
            call?.proxy
              ? `${call.proxy}#habitat`
              : instanceProxy(oauthSession, env, nsid),
          ) as XrpcOptions<M>;
          const res = yield* Effect.tryPromise({
            try: () => lexXrpc(agent, method, opts),
            catch: (cause) => toHabitatError(did, nsid, cause, !!call?.proxy),
          });
          return res.body;
        }).pipe(
          Effect.withSpan("Habitat.xrpc", {
            attributes: { did, nsid: methodNsid(method) },
          }),
        );

      const callNsid = <T>(
        did: string,
        nsid: string,
        method: "GET" | "POST",
        payload: Record<string, unknown>,
        call?: CallOptions,
      ) =>
        Effect.tryPromise({
          try: () =>
            new HabitatClient(env, did).call<T>(nsid, method, payload, {
              atprotoProxy: call?.proxy ? `${call.proxy}#habitat` : undefined,
            }),
          catch: (cause) => toHabitatError(did, nsid, cause, !!call?.proxy),
        }).pipe(Effect.withSpan("Habitat.call", { attributes: { did, nsid } }));

      return Habitat.of({
        session,
        xrpc,
        call: callNsid,
        client: (did) => new HabitatClient(env, did),
      });
    }),
  );
}

// methodNsid reads a lexicon method's NSID off its schema, for logs and
// error messages.
function methodNsid(method: unknown): string {
  const main = Predicate.hasProperty(method, "main") ? method.main : method;
  return Predicate.hasProperty(main, "nsid") && Predicate.isString(main.nsid)
    ? main.nsid
    : "unknown";
}

function withProxy<O extends { headers?: HeadersInit }>(
  options: O,
  proxy: string | undefined,
): O {
  if (!proxy) return options;
  const headers = new Headers(options.headers);
  headers.set("atproto-proxy", proxy);
  return { ...options, headers };
}

// toHabitatError classifies whatever a call threw: a 401 (or a session that
// couldn't be restored) is SessionExpired, anything else a HabitatError
// carrying pear's status and XRPC error name when there was a response.
//
// proxied is true for a call made to a group through the member's pear
// (atproto-proxy). Pear answers a denied action there with a 401 whose body
// the proxy strips, so it can't be told from a lapsed session; the member's
// own session was restored fine to get that far (a refresh failure is an
// UnauthorizedError, handled above), so it is reported as a refusal rather
// than signing the member out.
export function toHabitatError(
  did: string,
  nsid: string,
  cause: unknown,
  proxied = false,
): HabitatError | SessionExpired {
  for (let e = cause; e instanceof Error; e = e.cause) {
    if (e instanceof UnauthorizedError) {
      return new SessionExpired({ did: e.did, message: e.message });
    }
  }
  if (cause instanceof XrpcError) {
    const status = Predicate.hasProperty(cause, "status")
      ? Number(cause.status)
      : 0;
    if (status === 401 && !proxied) {
      return new SessionExpired({ did, message: messageOf(cause) });
    }
    return new HabitatError({
      nsid,
      status: status === 401 ? 403 : status,
      error: cause.error,
      message: messageOf(cause),
    });
  }
  const status = statusFromMessage(messageOf(cause));
  return new HabitatError({ nsid, status, message: messageOf(cause) });
}

// HabitatClient reports failures as "<nsid> failed (<status>): <body>".
function statusFromMessage(message: string): number {
  const match = /failed \((\d{3})\)/.exec(message);
  return match ? Number(match[1]) : 0;
}

export function messageOf(cause: unknown): string {
  return Predicate.hasProperty(cause, "message") &&
    Predicate.isString(cause.message)
    ? cause.message
    : String(cause);
}

export const AppLayer = Habitat.layer.pipe(
  Layer.provideMerge(Layer.mergeAll(Database.layer, OAuth.layer)),
  Layer.provideMerge(AppEnv.layer),
);

// attempt runs Promise-based code that calls the Habitat instance (the
// helpers ported from Chalk and Drop) as an Effect, classifying what it
// throws the same way Habitat's own calls are: SessionExpired for a lapsed
// session, HabitatError for anything else.
export function attempt<A>(
  did: string,
  label: string,
  f: () => Promise<A>,
): Effect.Effect<A, HabitatError | SessionExpired> {
  return Effect.tryPromise({
    try: f,
    catch: (cause) => toHabitatError(did, label, cause),
  }).pipe(Effect.withSpan(label));
}
