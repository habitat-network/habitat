import { env as workerEnv } from "cloudflare:workers";
import {
  xrpc as lexXrpc,
  XrpcError,
  type Agent,
  type LexValue,
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
import { type HabitatClient, instanceProxy } from "@/server/habitatClient";
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
  // (sent as Atproto-Proxy). group.opensocial.* and other
  // management-plane methods are served by the group's own Habitat
  // instance, which a member's session doesn't necessarily talk to directly.
  readonly proxy?: string;
}

// Habitat makes calls to the Habitat instance as a specific DID (a member,
// or a group a member has connected), using the OAuth session the app holds
// for it. It is the app's one XRPC client: every failure is typed at the
// source, SessionExpired when pear no longer accepts the session,
// HabitatError (with the HTTP status and XRPC error name) for anything else.
export interface HabitatService {
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
  // uploadBlob uploads raw bytes to the DID's own repo.
  readonly uploadBlob: (
    did: string,
    bytes: Uint8Array,
    mimeType: string,
  ) => Effect.Effect<
    { blob: LexValue; cid: string },
    HabitatError | SessionExpired
  >;
  // getBlob reads a blob from a space repo, as the DID.
  readonly getBlob: (
    did: string,
    space: string,
    repo: string,
    cid: string,
  ) => Effect.Effect<Uint8Array, HabitatError | SessionExpired>;
  // client is the same service as a Promise-based HabitatClient for the DID,
  // for the code that isn't written as Effects (the doc helpers, comments).
  readonly client: (did: string) => HabitatClient;
}

export class Habitat extends Context.Service<Habitat, HabitatService>()(
  "app/Habitat",
) {
  static readonly layer = Layer.effect(
    Habitat,
    Effect.gen(function* () {
      return makeHabitat(yield* AppEnv, yield* OAuth);
    }),
  );
}

// makeHabitat builds the service from the Worker's bindings and an OAuth
// client; Habitat.layer provides the app's, and createHabitatClient (for
// code outside an Effect runtime) builds its own.
export function makeHabitat(env: Env, oauth: OAuthClient): HabitatService {
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
        proxyFor(oauthSession, env, nsid, call),
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

  // fetchXrpc sends one raw request through the DID's session and fails with
  // the typed error for any non-2xx response.
  const fetchXrpc = (
    did: string,
    nsid: string,
    init: { method: "GET" | "POST"; query?: URLSearchParams; body?: BodyInit },
    headers: Record<string, string>,
    call?: CallOptions,
  ) =>
    Effect.gen(function* () {
      const oauthSession = yield* session(did);
      const proxy = proxyFor(oauthSession, env, nsid, call);
      if (proxy) headers["atproto-proxy"] = proxy;
      const path = `/xrpc/${nsid}${init.query ? `?${init.query}` : ""}`;
      const res = yield* Effect.tryPromise({
        try: () =>
          oauthSession.fetchHandler(path, {
            method: init.method,
            body: init.body,
            headers,
          }),
        catch: (cause) => toHabitatError(did, nsid, cause, !!call?.proxy),
      });
      if (!res.ok) {
        const text = yield* Effect.promise(() => res.text());
        return yield* failure(did, nsid, res.status, text, !!call?.proxy);
      }
      return res;
    });

  const callNsid = <T>(
    did: string,
    nsid: string,
    method: "GET" | "POST",
    payload: Record<string, unknown>,
    call?: CallOptions,
  ) =>
    Effect.gen(function* () {
      let query: URLSearchParams | undefined;
      let body: string | undefined;
      const headers: Record<string, string> = {};
      if (method === "GET") {
        query = new URLSearchParams();
        for (const [k, v] of Object.entries(payload)) {
          if (v !== undefined && v !== null) query.set(k, String(v));
        }
      } else {
        body = JSON.stringify(payload);
        headers["content-type"] = "application/json";
      }
      const res = yield* fetchXrpc(
        did,
        nsid,
        { method, query, body },
        headers,
        call,
      );
      // A procedure with no output schema (e.g.
      // network.habitat.relationship.deleteRelation) returns an empty 200
      // body, which res.json() would throw on.
      const text = yield* Effect.promise(() => res.text());
      return (text ? JSON.parse(text) : undefined) as T;
    }).pipe(Effect.withSpan("Habitat.call", { attributes: { did, nsid } }));

  const uploadBlob = (did: string, bytes: Uint8Array, mimeType: string) =>
    Effect.gen(function* () {
      const res = yield* fetchXrpc(
        did,
        "network.habitat.repo.uploadBlob",
        { method: "POST", body: bytes as BodyInit },
        { "content-type": mimeType },
      );
      return (yield* Effect.promise(() => res.json())) as {
        blob: LexValue;
        cid: string;
      };
    }).pipe(Effect.withSpan("Habitat.uploadBlob", { attributes: { did } }));

  const getBlob = (did: string, space: string, repo: string, cid: string) =>
    Effect.gen(function* () {
      const res = yield* fetchXrpc(
        did,
        "network.habitat.space.getBlob",
        { method: "GET", query: new URLSearchParams({ space, repo, cid }) },
        {},
      );
      return new Uint8Array(yield* Effect.promise(() => res.arrayBuffer()));
    }).pipe(Effect.withSpan("Habitat.getBlob", { attributes: { did } }));

  const service: HabitatService = {
    session,
    xrpc,
    call: callNsid,
    uploadBlob,
    getBlob,
    client: (did) => promiseClient(service, did),
  };
  return service;
}

// promiseClient is the Promise view of the service for one DID. A failed
// call rejects with the typed SessionExpired or HabitatError itself.
export function promiseClient(
  habitat: HabitatService,
  did: string,
): HabitatClient {
  return {
    call: (nsid, method, payload, opts) =>
      Effect.runPromise(habitat.call(did, nsid, method, payload, opts)),
    uploadBlob: (bytes, mimeType) =>
      Effect.runPromise(habitat.uploadBlob(did, bytes, mimeType)),
    getBlob: (space, repo, cid) =>
      Effect.runPromise(habitat.getBlob(did, space, repo, cid)),
    asDid: (other) => promiseClient(habitat, other),
  };
}

function proxyFor(
  session: OAuthSession,
  env: Env,
  nsid: string,
  call?: CallOptions,
): string | undefined {
  return call?.proxy
    ? `${call.proxy}#habitat`
    : instanceProxy(session, env, nsid);
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

// failure is the typed error for a non-2xx response: a 401 is SessionExpired
// (pear no longer accepts the session, after the OAuth client's own
// refresh-and-retry), anything else a HabitatError carrying the status and
// the XRPC error name when the body names one.
//
// proxied is true for a call made to a group through the member's pear
// (atproto-proxy). Pear answers a denied action there with a 401 whose body
// the proxy strips, so it can't be told from a lapsed session; the member's
// own session was restored fine to get that far, so it is reported as a
// refusal rather than signing the member out.
export function failure(
  did: string,
  nsid: string,
  status: number,
  body: string,
  proxied: boolean,
) {
  const message = `${nsid} failed (${status}): ${body}`;
  if (status === 401 && !proxied) {
    return Effect.fail(new SessionExpired({ did, message }));
  }
  let error: unknown;
  let detail: unknown;
  try {
    const parsed = JSON.parse(body) as { error?: unknown; message?: unknown };
    error = parsed.error;
    detail = parsed.message;
  } catch {
    // not JSON; there is no error name
  }
  return Effect.fail(
    new HabitatError({
      nsid,
      status: status === 401 ? 403 : status,
      error: Predicate.isString(error) ? error : undefined,
      message,
      detail: Predicate.isString(detail) && detail ? detail : undefined,
    }),
  );
}

// toHabitatError classifies whatever a call threw: the typed errors pass
// through, an XrpcError from the typed client is classified like a raw
// response, and anything else is a HabitatError with status 0.
export function toHabitatError(
  did: string,
  nsid: string,
  cause: unknown,
  proxied = false,
): HabitatError | SessionExpired {
  if (cause instanceof SessionExpired || cause instanceof HabitatError) {
    return cause;
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
  return new HabitatError({ nsid, status: 0, message: messageOf(cause) });
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
