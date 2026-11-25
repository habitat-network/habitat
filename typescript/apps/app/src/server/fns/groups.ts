import { createServerFn } from "@tanstack/react-start";
import type { DidString, NsidString, SpaceRefString } from "@atproto/syntax";
import { com, community, network } from "api";
import { Effect, Schema } from "effect";
import { isConnectedOrg } from "@/db";
import { BadRequest } from "@/server/effect/errors";
import {
  currentMember,
  myOrgs,
  refreshMemberships,
  requireOrg,
} from "@/server/effect/member";
import { runEffect } from "@/server/effect/run";
import { Database, Habitat } from "@/server/effect/services";
import {
  getProfiles,
  groupNames,
  listMembers,
  membersSpace,
  readGroupProfile,
  readPermissions,
  viaGroup,
  viaInstance,
  type GroupProfile,
} from "@/server/groups";

// Groups as the signed-in member sees them: the ones they belong to, the
// one they're in, creating one, and joining one they were invited to.

const OrgInput = Schema.Struct({ org: Schema.String });

export interface GroupSummary {
  did: string;
  name: string | null;
  // Whether an admin has connected the group to this app (it holds the
  // group's own session), which docs and files need.
  connected: boolean;
}

export const listMyGroups = createServerFn({ method: "GET" }).handler(() =>
  runEffect(
    "groups.listMine",
    Effect.gen(function* () {
      const did = yield* currentMember;
      const orgs = yield* myOrgs();
      const names = yield* groupNames(did, orgs);
      return orgs.map((org): GroupSummary => ({
        did: org,
        name: names.get(org)?.name ?? null,
        connected: names.get(org)?.connected ?? false,
      }));
    }),
  ),
);

export interface GroupView extends GroupProfile {
  did: string;
  connected: boolean;
  // The signed-in member's roles in the group.
  myRoles: string[];
  // The actions the member's roles grant (community.opensocial.permissions
  // bindings), so pages can hide controls the member can't use. pear checks
  // every action again on its own.
  myActions: string[];
  memberCount: number;
  me: {
    displayName: string | null;
    handle: string | null;
    avatarUrl: string | null;
  };
}

// getGroup is everything the group's layout needs: its profile, whether
// it's connected, and what the member can do in it.
export const getGroup = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(OrgInput))
  .handler(({ data }) =>
    runEffect(
      "groups.get",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const db = yield* Database;
        const [profile, members, permissions, connected] = yield* Effect.all(
          [
            readGroupProfile(did, org),
            listMembers(did, org).pipe(
              Effect.catch((e) =>
                Effect.logWarning("listing members failed", e).pipe(
                  Effect.as([]),
                ),
              ),
            ),
            readPermissions(did, org).pipe(
              Effect.catch(() =>
                Effect.succeed({ exists: false, bindings: [], assignable: [] }),
              ),
            ),
            Effect.promise(() => isConnectedOrg(db, org)),
          ],
          { concurrency: "unbounded" },
        );
        const mine = members.find((m) => m.did === did);
        const myRoles = mine?.roles ?? [];
        // A group with no permissions record lets admins do everything (as
        // pear's CheckAction does); otherwise the bindings decide.
        const isAdmin = !permissions.exists && myRoles.includes("admin");
        const myActions = permissions.bindings
          .filter((b) => b.roles.some((r) => myRoles.includes(r)))
          .map((b) => b.action);
        return {
          did: org,
          ...profile,
          connected,
          myRoles,
          myActions: isAdmin
            ? [...new Set([...myActions, ...ALL_ACTIONS])]
            : myActions,
          memberCount: members.length,
          me: {
            displayName: mine?.displayName ?? null,
            handle: mine?.handle ?? null,
            avatarUrl: mine?.avatarUrl ?? null,
          },
        } satisfies GroupView;
      }),
    ),
  );

export const ALL_ACTIONS = [
  "invite",
  "eject",
  "role.assign",
  "space.create",
  "space.configure",
  "space.delete",
  "community.configure",
  "mcp.configure",
];

// createGroup mints a new group with the member as its admin, then writes
// the member's own acceptance record, so the group shows up in their own
// list the same way it does for an invited member.
export const createGroup = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({
        handle: Schema.String,
        name: Schema.String,
        description: Schema.optional(Schema.String),
      }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "groups.create",
      Effect.gen(function* () {
        const did = yield* currentMember;
        const habitat = yield* Habitat;
        const handle = data.handle.trim().toLowerCase();
        if (!handle) return yield* new BadRequest({ message: "Pick a handle" });
        const { org } = yield* habitat
          .xrpc(
            did,
            network.habitat.opensocial.createOrg.main,
            { body: { handle } },
            yield* viaInstance,
          )
          .pipe(
            Effect.catchTag("HabitatError", (e) =>
              e.error === "HandleTaken"
                ? Effect.fail(
                    new BadRequest({
                      message: `The handle "${handle}" is already taken. Pick another one.`,
                    }),
                  )
                : Effect.fail(e),
            ),
          );
        yield* accept(did, org);
        if (data.name.trim()) {
          yield* habitat.xrpc(
            did,
            community.opensocial.updateProfile.main,
            {
              body: {
                org: org as DidString,
                name: data.name.trim(),
                description: data.description?.trim() || undefined,
              },
            },
            viaGroup(org),
          );
        }
        yield* refreshMemberships();
        yield* Effect.logInfo("created group", { org, handle });
        return { org };
      }),
    ),
  );

// accept redeems the member's invite to org (requestJoin) and writes their
// own acceptance record into the group's members space: joining is the
// member's own act, so it's authored under their credentials.
const accept = Effect.fn("groups.accept")(function* (did: string, org: string) {
  const habitat = yield* Habitat;
  const { roles } = yield* habitat.xrpc(
    did,
    community.opensocial.requestJoin.main,
    { body: { org: org as DidString } },
    viaGroup(org),
  );
  yield* habitat.xrpc(did, com.atproto.space.putRecord.main, {
    body: {
      space: membersSpace(org) as SpaceRefString,
      repo: did as DidString,
      collection: "community.opensocial.acceptance" as NsidString,
      rkey: "self",
      record: {
        $type: "community.opensocial.acceptance",
        updatedAt: new Date().toISOString(),
      },
    },
  });
  return roles;
});

export interface InviteView {
  id: string;
  org: string;
  orgName: string | null;
  roles: string[];
  createdAt: string;
}

// listMyInvites lists the member's pending invites, across every group on
// this instance.
export const listMyInvites = createServerFn({ method: "GET" }).handler(() =>
  runEffect(
    "groups.listMyInvites",
    Effect.gen(function* () {
      const did = yield* currentMember;
      const habitat = yield* Habitat;
      const { invites } = yield* habitat.xrpc(
        did,
        community.opensocial.listInvites.main,
        { params: {} },
        yield* viaInstance,
      );
      const names = yield* Effect.forEach(
        invites,
        (i) => readGroupProfile(did, i.org).pipe(Effect.map((p) => p.name)),
        { concurrency: 8 },
      );
      return invites.map((i, n): InviteView => ({
        id: i.id,
        org: i.org,
        orgName: names[n] ?? null,
        roles: i.roles,
        createdAt: i.createdAt,
      }));
    }),
  ),
);

export const acceptInvite = createServerFn({ method: "POST" })
  .validator(Schema.decodeUnknownSync(OrgInput))
  .handler(({ data }) =>
    runEffect(
      "groups.acceptInvite",
      Effect.gen(function* () {
        const did = yield* currentMember;
        const roles = yield* accept(did, data.org);
        yield* refreshMemberships();
        yield* Effect.logInfo("joined group", { org: data.org, roles });
        return { roles };
      }),
    ),
  );

// updateGroupProfile replaces the group's name and description. pear
// requires the community.configure action.
export const updateGroupProfile = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({
        org: Schema.String,
        name: Schema.String,
        description: Schema.String,
      }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "groups.updateProfile",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        yield* habitat.xrpc(
          did,
          community.opensocial.updateProfile.main,
          {
            body: {
              org,
              name: data.name,
              description: data.description || undefined,
            },
          },
          viaGroup(org),
        );
      }),
    ),
  );

// putMyGroupProfile writes the member's own profile within the group (a
// community.opensocial.memberProfile record in the members space, keyed by
// their DID), which is what other members see.
export const putMyGroupProfile = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({
        org: Schema.String,
        displayName: Schema.String,
        bio: Schema.String,
      }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "groups.putMyProfile",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        const existing = (yield* getProfiles(did, org, [did])).get(did);
        yield* habitat.xrpc(did, com.atproto.space.putRecord.main, {
          body: {
            space: membersSpace(org) as SpaceRefString,
            repo: did,
            collection: "community.opensocial.memberProfile" as NsidString,
            rkey: did,
            record: {
              $type: "community.opensocial.memberProfile",
              displayName: data.displayName.trim() || undefined,
              bio: data.bio.trim() || undefined,
              avatarUrl: existing?.avatarUrl || undefined,
              updatedAt: new Date().toISOString(),
            },
          },
        });
      }),
    ),
  );

export const getMyGroupProfile = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(OrgInput))
  .handler(({ data }) =>
    runEffect(
      "groups.getMyProfile",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const p = (yield* getProfiles(did, org, [did])).get(did);
        return {
          displayName: p?.displayName ?? "",
          bio: p?.bio ?? "",
          handle: p?.handle ?? null,
          avatarUrl: p?.avatarUrl ?? null,
        };
      }),
    ),
  );

// leaveGroup removes the member from the group.
export const leaveGroup = createServerFn({ method: "POST" })
  .validator(Schema.decodeUnknownSync(OrgInput))
  .handler(({ data }) =>
    runEffect(
      "groups.leave",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        yield* habitat.xrpc(
          did,
          community.opensocial.ejectMember.main,
          { body: { org, member: did as DidString } },
          viaGroup(org),
        );
        yield* refreshMemberships();
      }),
    ),
  );
