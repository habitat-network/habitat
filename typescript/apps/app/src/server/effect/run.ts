import { redirect } from "@tanstack/react-router";
import {
  Cause,
  Effect,
  Exit,
  Layer,
  Logger,
  ManagedRuntime,
  Option,
} from "effect";
import { useAppSession, type AppSessionData } from "@/server/session";
import { type AppError, userMessage } from "./errors";
import { CurrentSession } from "./member";
import { AppLayer } from "./services";

// The bridge between TanStack Start and Effect. Server functions and server
// routes describe their work as Effects over the app's services; runEffect
// and runHandler run them on one runtime per isolate, with:
//
// - logging: every log line carries the function's name and the signed-in
//   member, and a span with its duration. Production logs are one JSON
//   object per line, which Workers Logs indexes field by field.
// - error handling: expected failures (./errors.ts) become a redirect to
//   /login, a sign-out, or an Error whose message the page shows; anything
//   else is a defect, logged with its full cause and reported to the browser
//   only as a generic message.

const LoggerLayer = Logger.layer([
  import.meta.env.DEV ? Logger.consolePretty() : Logger.consoleJson,
]);

const runtime = ManagedRuntime.make(Layer.mergeAll(AppLayer, LoggerLayer));

export type AppServices = ManagedRuntime.ManagedRuntime.Services<
  typeof runtime
>;

type Program<A> = Effect.Effect<A, AppError, AppServices | CurrentSession>;

const GENERIC_FAILURE = "Something went wrong on our side. Try again.";

// sessionService wraps the request's session cookie for CurrentSession.
async function sessionService() {
  const cookie = await useAppSession();
  const data: AppSessionData = { ...cookie.data };
  return {
    cookie,
    service: CurrentSession.of({
      data,
      update: (patch) =>
        Effect.promise(async () => {
          Object.assign(data, patch);
          await cookie.update(patch);
        }),
      clear: Effect.promise(async () => {
        data.did = undefined;
        await cookie.clear();
      }),
    }),
  };
}

async function execute<A>(name: string, effect: Program<A>) {
  const { cookie, service } = await sessionService();
  const exit = await runtime.runPromiseExit(
    effect.pipe(
      Effect.tapCause(logFailure),
      Effect.withLogSpan(name),
      Effect.annotateLogs({ fn: name, member: cookie.data.did ?? "anonymous" }),
      Effect.withSpan(name),
      Effect.provideService(CurrentSession, service),
    ),
  );
  return { exit, cookie };
}

// logFailure logs a failed Effect at a level matching how surprising it is:
// a redirect or a missing session is routine, an expected error a warning,
// and a defect an error with its full cause.
function logFailure(cause: Cause.Cause<AppError>) {
  const error = Cause.findErrorOption(cause);
  if (Option.isNone(error)) {
    return Effect.logError("unexpected failure", Cause.pretty(cause));
  }
  switch (error.value._tag) {
    case "NotSignedIn":
    case "Redirect":
      return Effect.logDebug(error.value._tag);
    default:
      return Effect.logWarning(error.value._tag, error.value);
  }
}

// runEffect runs a server function's Effect and returns what it succeeds
// with, or throws what TanStack Start expects for its failure: a redirect
// (to /login when the member isn't signed in or their session has expired),
// or an Error carrying a message fit for the page to show.
export async function runEffect<A>(
  name: string,
  effect: Program<A>,
): Promise<A> {
  const { exit, cookie } = await execute(name, effect);
  if (Exit.isSuccess(exit)) return exit.value;
  const error = Cause.findErrorOption(exit.cause);
  if (Option.isNone(error)) throw new Error(GENERIC_FAILURE);
  switch (error.value._tag) {
    case "NotSignedIn":
      throw redirect({ to: "/login" });
    case "SessionExpired":
      // The member's own session lapsing ends their login. A group's
      // lapsing doesn't; it's reported like any other failure.
      if (error.value.did === cookie.data.did) {
        await cookie.clear();
        throw redirect({ to: "/login" });
      }
      throw new Error(userMessage(error.value));
    case "Redirect":
      throw redirect({ href: error.value.to });
    default:
      throw new Error(userMessage(error.value));
  }
}

const STATUS: Record<AppError["_tag"], number> = {
  NotSignedIn: 401,
  SessionExpired: 401,
  HabitatError: 502,
  NotAMember: 404,
  OrgNotConnected: 409,
  Forbidden: 403,
  NotFound: 404,
  BadRequest: 400,
  Redirect: 302,
};

// runHandler runs a server route's Effect (uploads, downloads, the doc
// WebSocket), answering an expected failure with a JSON error and a status
// code instead of a redirect.
export async function runHandler(
  name: string,
  effect: Program<Response>,
): Promise<Response> {
  const { exit } = await execute(name, effect);
  if (Exit.isSuccess(exit)) return exit.value;
  const error = Cause.findErrorOption(exit.cause);
  if (Option.isNone(error)) {
    return Response.json({ error: GENERIC_FAILURE }, { status: 500 });
  }
  if (error.value._tag === "Redirect") {
    return new Response(null, {
      status: 302,
      headers: { location: error.value.to },
    });
  }
  const status =
    error.value._tag === "HabitatError" && error.value.status === 413
      ? 413
      : STATUS[error.value._tag];
  return Response.json({ error: userMessage(error.value) }, { status });
}

// runBackground runs an Effect with no request behind it (the cron trigger)
// on the same runtime and logger.
export function runBackground(
  name: string,
  effect: Effect.Effect<void, unknown, AppServices>,
): Promise<void> {
  return runtime.runPromise(
    effect.pipe(
      Effect.catchCause((cause) =>
        Effect.logError("background job failed", Cause.pretty(cause)),
      ),
      Effect.withLogSpan(name),
      Effect.annotateLogs({ fn: name }),
      Effect.withSpan(name),
    ),
  );
}
