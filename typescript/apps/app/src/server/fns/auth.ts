import { createServerFn } from "@tanstack/react-start";
import type { DidString } from "@atproto/syntax";
import { Effect, Schema } from "effect";
import { upsertConnectedOrg } from "@/db";
import { BadRequest, Forbidden } from "@/server/effect/errors";
import {
  CurrentSession,
  currentMember,
  refreshMemberships,
  requireOrg,
} from "@/server/effect/member";
import { runEffect } from "@/server/effect/run";
import { AppEnv, Database, OAuth, messageOf } from "@/server/effect/services";
import { readGroupProfile } from "@/server/groups";
import type { OAuthAppState } from "@/server/habitatClient";
import { EMAIL_DOMAIN_NOT_FOUND_MESSAGE } from "internal";
import { OAUTH_SCOPE } from "@/server/oauth";
import { followOrg } from "@/server/stream";

// Sign-in, sign-out, and connecting a group. The app is a plain atproto
// OAuth client of the Habitat instance: members sign in with their handle
// (or work email), and an admin connects a group by running the same flow
// with the group's DID, which pear routes through its admin approval. Both
// sessions are stored in D1 under their DID.

// authorize begins an OAuth flow for loginHint, returning the URL the
// browser should go to next.
const authorize = Effect.fn("authorize")(function* (
  loginHint: string,
  state: OAuthAppState,
) {
  const oauth = yield* OAuth;
  const url = yield* Effect.tryPromise({
    try: () =>
      oauth.authorize(loginHint, {
        scope: OAUTH_SCOPE,
        state: JSON.stringify(state),
      }),
    catch: (cause) =>
      new BadRequest({
        message:
          loginHint.includes("@") && isDidNotFound(cause)
            ? EMAIL_DOMAIN_NOT_FOUND_MESSAGE
            : `Couldn't start sign-in for ${loginHint}: ${messageOf(cause)}`,
      }),
  });
  return { redirectUrl: url.toString() };
});

// isDidNotFound reports whether the identity resolver said a work email's
// domain isn't mapped to any group.
function isDidNotFound(err: unknown): boolean {
  for (let e = err; e instanceof Error; e = e.cause) {
    if ((e as { xrpcError?: unknown }).xrpcError === "DidNotFound") return true;
  }
  return false;
}

export const startLogin = createServerFn({ method: "POST" })
  .validator(Schema.decodeUnknownSync(Schema.Struct({ handle: Schema.String })))
  .handler(({ data }) =>
    runEffect(
      "auth.startLogin",
      authorize(data.handle.trim(), { kind: "member" }),
    ),
  );

// startGroupConnect begins the OAuth flow for the group's own identity.
// pear shows its admin-approval page (only an admin can complete it), then
// redirects back to /oauth/callback.
export const startGroupConnect = createServerFn({ method: "POST" })
  .validator(Schema.decodeUnknownSync(Schema.Struct({ org: Schema.String })))
  .handler(({ data }) =>
    runEffect(
      "auth.startGroupConnect",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        return yield* authorize(org, {
          kind: "org",
          orgDid: org,
          memberDid: did,
        });
      }),
    ),
  );

// completeOAuth finishes whichever flow just redirected back to
// /oauth/callback and says where to send the browser next.
//
// - A member sign-in sets the session cookie and remembers their groups.
// - A group connect checks the session that came back really is the
//   group's, and that the member who started it is still the one signed in,
//   then records the group as connected.
//
// A connected group's stream starts at once: best-effort, since a hiccup
// there shouldn't fail the connect (the cron run starts it anyway).
export const completeOAuth = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({ params: Schema.Record(Schema.String, Schema.String) }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "auth.completeOAuth",
      Effect.gen(function* () {
        const oauth = yield* OAuth;
        const env = yield* AppEnv;
        const session = yield* CurrentSession;
        const { session: oauthSession, state } = yield* Effect.tryPromise({
          try: () => oauth.callback(new URLSearchParams(data.params)),
          catch: (cause) =>
            new BadRequest({ message: `Sign-in failed: ${messageOf(cause)}` }),
        });
        const appState: OAuthAppState = state
          ? (JSON.parse(state) as OAuthAppState)
          : { kind: "member" };

        if (appState.kind === "member") {
          yield* session.update({ did: oauthSession.did });
          yield* refreshMemberships().pipe(
            Effect.catch((e) => Effect.logWarning("listing groups failed", e)),
          );
          yield* Effect.logInfo("member signed in", { did: oauthSession.did });
          return { to: "/" };
        }

        if (session.data.did !== appState.memberDid) return { to: "/login" };
        if (oauthSession.did !== appState.orgDid) {
          return yield* new Forbidden({
            message:
              "You signed in as a different identity than the group being connected",
          });
        }
        const org = oauthSession.did;
        const profile = yield* readGroupProfile(org, org);
        const db = yield* Database;
        yield* Effect.promise(() =>
          upsertConnectedOrg(db, {
            orgDid: org,
            name: profile.name ?? org,
            connectedBy: appState.memberDid,
          }),
        );
        // Follow the group's stream from now on: it replays every record
        // the group can read into D1, then follows it live.
        yield* Effect.tryPromise(() => followOrg(env, org)).pipe(
          Effect.catch((e) => Effect.logWarning("following group failed", e)),
        );
        yield* Effect.logInfo("group connected", {
          org,
          by: appState.memberDid,
        });
        return { to: `/g/${encodeURIComponent(org)}` };
      }),
    ),
  );

export interface Member {
  did: string;
  handle: string | null;
}

// getMember returns the signed-in member, redirecting to /login if none.
export const getMember = createServerFn({ method: "GET" }).handler(() =>
  runEffect(
    "auth.getMember",
    Effect.gen(function* () {
      const did = yield* currentMember;
      const env = yield* AppEnv;
      const handle = yield* Effect.promise(() => resolveHandle(env, did));
      return { did, handle } satisfies Member;
    }),
  ),
);

export const signOut = createServerFn({ method: "POST" }).handler(() =>
  runEffect(
    "auth.signOut",
    Effect.gen(function* () {
      const session = yield* CurrentSession;
      const did = session.data.did;
      yield* session.clear;
      if (!did) return;
      // Best-effort: forget the member's tokens too. Group sessions are left
      // alone: they belong to the group, not to whoever connected it.
      const oauth = yield* OAuth;
      yield* Effect.tryPromise(() => oauth.revoke(did)).pipe(
        Effect.catch((e) =>
          Effect.logWarning("revoking member session failed", e),
        ),
      );
    }),
  ),
);

// resolveHandle looks up a DID's handle through the Habitat instance, for
// display. Null if it can't be resolved.
async function resolveHandle(env: Env, did: string): Promise<string | null> {
  const { HabitatIdentityResolver } = await import("@habitat-network/habitat");
  try {
    const info = await new HabitatIdentityResolver(env.APP_HABITAT_URL).resolve(
      did as DidString,
    );
    return info.handle === "handle.invalid" ? null : info.handle;
  } catch {
    return null;
  }
}
