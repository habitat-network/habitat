import { createServerFn } from "@tanstack/react-start";
import type { DidString } from "@atproto/syntax";
import { HabitatIdentityResolver } from "@habitat-network/habitat";
import { community } from "api";
import { Effect, Schema } from "effect";
import { BadRequest } from "@/server/effect/errors";
import { requireOrg } from "@/server/effect/member";
import { runEffect } from "@/server/effect/run";
import { AppEnv, Habitat } from "@/server/effect/services";
import { MEMBERSHIP_COLLECTION } from "@/server/members/sink";
import { awaitIndexed } from "@/server/stream";
import {
  getProfiles,
  listMembers,
  listRoles,
  membersSpace,
  readPermissions,
  viaGroup,
  type ActionBinding,
  type AssignableBinding,
  type MemberView,
  type PermissionsView,
  type RoleView,
} from "@/server/groups";

// Managing a group: its roster and invites, its roles and what they're
// allowed to do. Every write is an opensocial call made as the signed-in
// member, so pear decides whether they hold the action it needs (invite,
// eject, role.assign, community.configure).

export type {
  ActionBinding,
  AssignableBinding,
  MemberView,
  PermissionsView,
  RoleView,
};

const OrgInput = Schema.Struct({ org: Schema.String });

export const listGroupMembers = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(OrgInput))
  .handler(({ data }) =>
    runEffect(
      "members.list",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        return yield* listMembers(did, org);
      }),
    ),
  );

export interface PendingInviteView {
  id: string;
  invitee: string;
  inviteeHandle: string | null;
  inviteeName: string | null;
  roles: string[];
  createdAt: string;
}

// listGroupInvites lists the group's outstanding invites. pear requires the
// invite action; a member without it gets an empty list.
export const listGroupInvites = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(OrgInput))
  .handler(({ data }) =>
    runEffect(
      "members.listInvites",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        const result = yield* habitat
          .xrpc(
            did,
            community.opensocial.listPendingInvites.main,
            { params: { org } },
            viaGroup(org),
          )
          .pipe(
            Effect.catchTag("HabitatError", (e) =>
              e.status === 403
                ? Effect.succeed({ invites: [] })
                : Effect.fail(e),
            ),
          );
        const profiles = yield* getProfiles(
          did,
          org,
          result.invites.map((i) => i.invitee),
        );
        return result.invites.map((i): PendingInviteView => {
          const p = profiles.get(i.invitee);
          return {
            id: i.id,
            invitee: i.invitee,
            inviteeHandle: p?.handle ?? null,
            inviteeName: p?.displayName ?? null,
            roles: i.roles,
            createdAt: i.createdAt,
          };
        });
      }),
    ),
  );

// inviteMember invites someone, by handle or DID, to join with the given
// roles once they accept.
export const inviteMember = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({
        org: Schema.String,
        invitee: Schema.String,
        roles: Schema.Array(Schema.String),
      }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "members.invite",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        const env = yield* AppEnv;
        const who = data.invitee.trim().replace(/^@/, "");
        const identity = yield* Effect.tryPromise({
          try: () =>
            new HabitatIdentityResolver(env.APP_HABITAT_URL).resolve(who),
          catch: () => new BadRequest({ message: `Couldn't find ${who}` }),
        });
        const { invite } = yield* habitat.xrpc(
          did,
          community.opensocial.createInvite.main,
          {
            body: {
              org,
              invitee: identity.did,
              roles: data.roles.length > 0 ? [...data.roles] : ["member"],
            },
          },
          viaGroup(org),
        );
        yield* Effect.logInfo("invited member", { org, invitee: identity.did });
        return invite;
      }),
    ),
  );

export const revokeInvite = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({ org: Schema.String, id: Schema.String }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "members.revokeInvite",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        yield* habitat.xrpc(
          did,
          community.opensocial.revokeInvite.main,
          { body: { org, id: data.id } },
          viaGroup(org),
        );
      }),
    ),
  );

// assignRoles sets a member's full role set. pear bounds it by the roles
// the caller may assign.
export const assignRoles = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({
        org: Schema.String,
        member: Schema.String,
        roles: Schema.Array(Schema.String),
      }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "members.assignRoles",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        yield* habitat.xrpc(
          did,
          community.opensocial.assignRoles.main,
          {
            body: {
              org,
              member: data.member as DidString,
              roles: [...data.roles],
            },
          },
          viaGroup(org),
        );
        yield* Effect.logInfo("assigned roles", {
          org,
          member: data.member,
          roles: data.roles,
        });
      }),
    ),
  );

export const removeMember = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({ org: Schema.String, member: Schema.String }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "members.remove",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        yield* habitat.xrpc(
          did,
          community.opensocial.ejectMember.main,
          { body: { org, member: data.member as DidString } },
          viaGroup(org),
        );
        // pear deletes the member's membership record from the roster;
        // wait for the group's stream to index that, so they lose access
        // here at once.
        const membership = `${membersSpace(org)}/${org}/${MEMBERSHIP_COLLECTION}/${data.member}`;
        yield* awaitIndexed(org, membership, null);
        yield* Effect.logInfo("removed member", { org, member: data.member });
      }),
    ),
  );

// --- Roles & access ---------------------------------------------------------

export interface RolesPayload {
  roles: (RoleView & { memberCount: number })[];
  permissions: PermissionsView;
}

export const getRolesAndPermissions = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(OrgInput))
  .handler(({ data }) =>
    runEffect(
      "roles.get",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const [roles, permissions, members] = yield* Effect.all(
          [
            listRoles(did, org),
            readPermissions(did, org),
            listMembers(did, org),
          ],
          { concurrency: "unbounded" },
        );
        return {
          roles: roles.map((r) => ({
            ...r,
            memberCount: members.filter((m) => m.roles.includes(r.id)).length,
          })),
          permissions,
        } satisfies RolesPayload;
      }),
    ),
  );

// putRole creates or renames a role. pear requires community.configure.
export const putRole = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({
        org: Schema.String,
        role: Schema.String,
        name: Schema.String,
        description: Schema.optional(Schema.String),
      }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "roles.put",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const role = data.role.trim();
        if (!/^[a-z0-9][a-z0-9-]{0,62}$/.test(role)) {
          return yield* new BadRequest({
            message: "Role ids are lowercase letters, digits and dashes",
          });
        }
        const habitat = yield* Habitat;
        yield* habitat.xrpc(
          did,
          community.opensocial.putRole.main,
          {
            body: {
              org,
              role,
              name: data.name.trim() || role,
              description: data.description?.trim() || undefined,
            },
          },
          viaGroup(org),
        );
      }),
    ),
  );

export const deleteRole = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({ org: Schema.String, role: Schema.String }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "roles.delete",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        yield* habitat.xrpc(
          did,
          community.opensocial.deleteRole.main,
          { body: { org, role: data.role } },
          viaGroup(org),
        );
      }),
    ),
  );

const Binding = Schema.Struct({
  action: Schema.String,
  roles: Schema.Array(Schema.String),
});
const Assignable = Schema.Struct({
  role: Schema.String,
  roles: Schema.Array(Schema.String),
});

// updatePermissions replaces which roles hold which actions and which roles
// each role may assign. pear requires community.configure.
export const updatePermissions = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({
        org: Schema.String,
        bindings: Schema.Array(Binding),
        assignable: Schema.Array(Assignable),
      }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "roles.updatePermissions",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const habitat = yield* Habitat;
        yield* habitat.xrpc(
          did,
          community.opensocial.updatePermissions.main,
          {
            body: {
              org,
              bindings: data.bindings.map((b) => ({
                action: b.action,
                roles: [...b.roles],
              })),
              assignable: data.assignable.map((a) => ({
                role: a.role,
                roles: [...a.roles],
              })),
            },
          },
          viaGroup(org),
        );
        yield* Effect.logInfo("updated permissions", { org });
      }),
    ),
  );
