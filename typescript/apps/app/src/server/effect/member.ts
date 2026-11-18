import { SpaceRef, type DidString } from "@atproto/syntax";
import { network } from "api";
import { Context, Effect } from "effect";
import type { AppSessionData } from "@/server/session";
import { NotAMember, NotSignedIn } from "./errors";
import { Habitat } from "./services";

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

export const MEMBERS_SPACE_TYPE = "community.opensocial.members";

// listMyOrgIds lists every group did currently belongs to. pear's listSpaces
// returns every members space the member ever wrote into, which includes
// groups they've left or been removed from, so each candidate is confirmed
// by reading the member's own membership record in it.
export const listMyOrgIds = Effect.fn("listMyOrgIds")(function* (did: string) {
  const habitat = yield* Habitat;
  const body = yield* habitat.xrpc(did, network.habitat.space.listSpaces.main, {
    params: { spaceType: MEMBERS_SPACE_TYPE },
  });
  const candidates = body.spaces.map((s) => SpaceRef.parse(s.uri).spaceDid);
  const confirmed = yield* Effect.forEach(
    candidates,
    (org) =>
      habitat
        .call(did, "network.habitat.space.getRecord", "GET", {
          space: new SpaceRef(org, MEMBERS_SPACE_TYPE, "self").toString(),
          repo: org,
          collection: "community.opensocial.membership",
          rkey: did,
        })
        .pipe(
          Effect.as(true),
          // A 4xx means no record for them or no access: not a member.
          // Anything else (pear down) must not read as "left every group".
          Effect.catchTag("HabitatError", (e) =>
            e.status >= 400 && e.status < 500
              ? Effect.succeed(false)
              : Effect.fail(e),
          ),
        ),
    { concurrency: 8 },
  );
  return candidates.filter((_, i) => confirmed[i]);
});

// refreshMemberships asks pear which groups the member belongs to and
// remembers the answer in the session cookie.
export const refreshMemberships = Effect.fn("refreshMemberships")(function* () {
  const did = yield* currentMember;
  const orgs = yield* listMyOrgIds(did);
  const session = yield* CurrentSession;
  yield* session.update({ orgs, orgsCheckedAt: Date.now() });
  return orgs;
});

// How long the session's cached group list is trusted before pear is asked
// again, so a member who leaves or is removed loses access within a minute.
const MEMBERSHIP_CACHE_MS = 60_000;

// requireOrg checks the signed-in member belongs to org, the gate every
// group-scoped server function starts with. The session's cached list
// answers most calls; a group missing from it (joined since sign-in) is
// checked with pear once, and the cache refreshed.
export const requireOrg = Effect.fn("requireOrg")(function* (org: string) {
  const did = yield* currentMember;
  const session = yield* CurrentSession;
  const { orgs: cached, orgsCheckedAt } = session.data;
  if (
    cached?.includes(org) &&
    orgsCheckedAt !== undefined &&
    Date.now() - orgsCheckedAt < MEMBERSHIP_CACHE_MS
  ) {
    return { did, org: org as DidString };
  }
  const orgs = yield* refreshMemberships();
  if (!orgs.includes(org as DidString)) {
    return yield* new NotAMember({ org });
  }
  return { did, org: org as DidString };
});
