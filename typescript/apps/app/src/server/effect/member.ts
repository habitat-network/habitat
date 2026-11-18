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

// listMyOrgIds lists every group did belongs to: every
// community.opensocial.members space they hold a membership or acceptance
// record in.
export const listMyOrgIds = Effect.fn("listMyOrgIds")(function* (did: string) {
  const habitat = yield* Habitat;
  const body = yield* habitat.xrpc(did, network.habitat.space.listSpaces.main, {
    params: { spaceType: MEMBERS_SPACE_TYPE },
  });
  return body.spaces.map((s) => SpaceRef.parse(s.uri).spaceDid);
});

// refreshMemberships asks pear which groups the member belongs to and
// remembers the answer in the session cookie.
export const refreshMemberships = Effect.fn("refreshMemberships")(function* () {
  const did = yield* currentMember;
  const orgs = yield* listMyOrgIds(did);
  const session = yield* CurrentSession;
  yield* session.update({ orgs });
  return orgs;
});

// requireOrg checks the signed-in member belongs to org, the gate every
// group-scoped server function starts with. The session's cached list
// answers most calls; a group missing from it (joined since sign-in) is
// checked with pear once, and the cache refreshed.
export const requireOrg = Effect.fn("requireOrg")(function* (org: string) {
  const did = yield* currentMember;
  const session = yield* CurrentSession;
  if (session.data.orgs?.includes(org)) {
    return { did, org: org as DidString };
  }
  const orgs = yield* refreshMemberships();
  if (!orgs.includes(org as DidString)) {
    return yield* new NotAMember({ org });
  }
  return { did, org: org as DidString };
});
