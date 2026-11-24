import { SpaceRef, type DidString } from "@atproto/syntax";
import { network } from "api";
import { Context, Effect } from "effect";
import type { AppSessionData } from "@/server/session";
import { NotAMember, NotSignedIn } from "./errors";
import {
  connectedOrgNames,
  isMemberOf,
  orgsOf,
  reconcileMemberships,
} from "@/db";
import {
  MEMBERS_SPACE_TYPE,
  MEMBERSHIP_COLLECTION,
} from "@/server/members/sink";
import { AppEnv, Database, Habitat } from "./services";

// Request-scoped services: what runEffect (./run.ts) reads off the request
// before running a server function's Effect.

// CurrentSession is the request's session cookie.
export class CurrentSession extends Context.Service<
  CurrentSession,
  {
    readonly data: AppSessionData;
    readonly update: (patch: Partial<AppSessionData>) => Effect.Effect<void>;
    readonly clear: Effect.Effect<void>;
  }
>()("app/CurrentSession") {}

// currentMember is the signed-in member's DID, failing with NotSignedIn when
// the request has no session.
export const currentMember = Effect.gen(function* () {
  const session = yield* CurrentSession;
  if (!session.data.did) return yield* new NotSignedIn();
  return session.data.did as DidString;
});

export { MEMBERS_SPACE_TYPE };

// confirmedMemberships asks pear which groups did belongs to, with the
// roles it holds in each. pear's listSpaces returns every members space the
// member ever wrote into, which includes groups they've left or been removed
// from, so each candidate is confirmed by reading the member's own
// membership record in it.
const confirmedMemberships = Effect.fn("confirmedMemberships")(function* (
  did: string,
) {
  const habitat = yield* Habitat;
  const body = yield* habitat.xrpc(did, network.habitat.space.listSpaces.main, {
    params: { spaceType: MEMBERS_SPACE_TYPE },
  });
  const candidates = body.spaces.map((s) => SpaceRef.parse(s.uri));
  const found = yield* Effect.forEach(
    candidates,
    (space) =>
      habitat
        .call<{ value?: { roles?: unknown } }>(
          did,
          "network.habitat.space.getRecord",
          "GET",
          {
            space: space.toString(),
            repo: space.spaceDid,
            collection: MEMBERSHIP_COLLECTION,
            rkey: did,
          },
        )
        .pipe(
          Effect.map((r) => ({
            orgDid: space.spaceDid as string,
            space: space.toString(),
            roles: Array.isArray(r.value?.roles)
              ? r.value.roles.filter((x): x is string => typeof x === "string")
              : [],
          })),
          // A 4xx means no record for them or no access: not a member.
          // Anything else (pear down) must not read as "left every group".
          Effect.catchTag("HabitatError", (e) =>
            e.status >= 400 && e.status < 500
              ? Effect.succeed(null)
              : Effect.fail(e),
          ),
        ),
    { concurrency: 8 },
  );
  return found.filter((f) => f !== null);
});

// refreshMemberships is the bootstrap: it asks pear which groups the member
// belongs to and makes their rows in the memberships table match. Every
// later change (a member removed, a role changed) to a connected group's
// roster arrives through the group's stream instead of being re-asked, so
// this also makes sure each connected group's stream is running.
export const refreshMemberships = Effect.fn("refreshMemberships")(function* () {
  const did = yield* currentMember;
  const db = yield* Database;
  const env = yield* AppEnv;
  const found = yield* confirmedMemberships(did);
  yield* Effect.promise(() => reconcileMemberships(db, did, found));
  // Imported lazily: the stream pulls in the sinks, which reach back into
  // the modules that import this one.
  yield* Effect.tryPromise(async () => {
    const { followOrg } = await import("@/server/stream");
    const connected = await connectedOrgNames(
      db,
      found.map((f) => f.orgDid),
    );
    await Promise.all([...connected.keys()].map((org) => followOrg(env, org)));
  }).pipe(Effect.catch((e) => Effect.logWarning("following groups failed", e)));
  return found.map((f) => f.orgDid as DidString);
});

// myOrgs lists the groups the member belongs to, as last synced; a member
// with none yet (just signed in, or just joined) is bootstrapped from pear.
// A group this deployment isn't connected to has no stream keeping its
// rows current (a removal would never reach them), so while the member
// holds any such group pear is asked again.
export const myOrgs = Effect.fn("myOrgs")(function* () {
  const did = yield* currentMember;
  const db = yield* Database;
  const orgs = yield* Effect.promise(() => orgsOf(db, did));
  if (orgs.length === 0) return yield* refreshMemberships();
  const connected = yield* Effect.promise(() => connectedOrgNames(db, orgs));
  if (orgs.some((org) => !connected.has(org)))
    return yield* refreshMemberships();
  return orgs as DidString[];
});

// requireOrg checks the signed-in member belongs to org, the gate every
// group-scoped server function starts with.
//
// It's also the request made "against" org that starts the org's event
// stream if it isn't running (see ../stream/orgStream.ts), and the stream
// runs first: the synced memberships table only answers once it's caught
// up, so a member removed while the stream was idle is refused. D1 is
// trusted only when the stream is "live". When it may be behind (the
// catch-up failed or is slow), the group isn't connected (no stream keeps
// its rows current, so a removed member would stay in them forever), or the
// member isn't in it (joined since the last sync), pear is asked instead.
export const requireOrg = Effect.fn("requireOrg")(function* (org: string) {
  const did = yield* currentMember;
  const db = yield* Database;
  const env = yield* AppEnv;
  // Imported lazily: the stream pulls in the sinks, which reach back into
  // the modules that import this one.
  const state = yield* Effect.tryPromise(async () => {
    const { followOrg } = await import("@/server/stream");
    return followOrg(env, org);
  }).pipe(
    Effect.catch((e) =>
      Effect.logWarning("following group failed", e).pipe(
        Effect.as("behind" as const),
      ),
    ),
  );
  const synced =
    state === "live" && (yield* Effect.promise(() => isMemberOf(db, org, did)));
  if (!synced) {
    const orgs = yield* refreshMemberships();
    if (!orgs.includes(org as DidString)) {
      return yield* new NotAMember({ org });
    }
  }
  return { did, org: org as DidString };
});
