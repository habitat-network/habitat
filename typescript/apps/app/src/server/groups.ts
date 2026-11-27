import { SpaceRef, type DidString, type NsidString } from "@atproto/syntax";
import { group, network, permissionsFromRecord } from "api";
import { Effect, Option } from "effect";
import { connectedOrgNames, isConnectedOrg } from "@/db";
import { HabitatError, OrgNotConnected } from "./effect/errors";
import { MEMBERS_SPACE_TYPE } from "./effect/member";
import { AppEnv, Database, Habitat, type CallOptions } from "./effect/services";

// Reads and writes of a group's opensocial records (its profile, roster,
// roles and permissions), shared by the server functions in ./fns.
// Management-plane calls are made as the signed-in member, proxied to the
// group's own Habitat instance, so pear's opensocial permission checks
// decide what each member may do; the app only shapes the results.

export const META_SPACE_TYPE = "group.opensocial.meta";

export function membersSpace(org: string): string {
  return new SpaceRef(org as DidString, MEMBERS_SPACE_TYPE, "self").toString();
}

export function metaSpace(org: string): string {
  return new SpaceRef(org as DidString, META_SPACE_TYPE, "self").toString();
}

// viaGroup proxies a call to the group's own Habitat instance.
export function viaGroup(org: string): CallOptions {
  return { proxy: org };
}

// viaInstance proxies a call to this app's Habitat instance, for
// management-plane calls not about one group (creating one, listing the
// member's invites).
export const viaInstance = Effect.gen(function* () {
  const env = yield* AppEnv;
  return { proxy: `did:web:${new URL(env.APP_HABITAT_URL).host}` };
});

export interface GroupProfile {
  name: string | null;
  description: string | null;
}

// readGroupProfile reads a group's group.opensocial.profile record as
// the member. Any member can read the group's about space. A profile that
// can't be read comes back empty rather than failing the page around it.
export const readGroupProfile = Effect.fn("readGroupProfile")(function* (
  did: string,
  org: string,
) {
  const habitat = yield* Habitat;
  const record = yield* habitat
    .call<{ value: Record<string, unknown> }>(
      did,
      "network.habitat.space.getRecord",
      "GET",
      {
        space: metaSpace(org),
        repo: org,
        collection: "group.opensocial.profile",
        rkey: "self",
      },
    )
    .pipe(Effect.option);
  const value = Option.match(record, {
    onNone: () => ({}) as Record<string, unknown>,
    onSome: (r) => r.value,
  });
  return {
    name: typeof value.displayName === "string" ? value.displayName : null,
    description:
      typeof value.description === "string" ? value.description : null,
  } satisfies GroupProfile;
});

// listGroupRecords lists the records of one collection in the group's own
// repo in its members space, as the member (members hold reader on it).
export const listGroupRecords = Effect.fn("listGroupRecords")(function* (
  did: string,
  org: string,
  collection: string,
) {
  const habitat = yield* Habitat;
  const body = yield* habitat.call<{
    records: { uri: string; rkey?: string; value: Record<string, unknown> }[];
  }>(did, "network.habitat.space.listRecords", "GET", {
    space: membersSpace(org),
    repo: org,
    collection: collection as NsidString,
    limit: 1000,
  });
  return body.records.map((r) => ({
    rkey: r.rkey ?? r.uri.split("/").pop() ?? "",
    value: r.value ?? {},
  }));
});

export interface MemberView {
  did: string;
  roles: string[];
  handle: string | null;
  displayName: string | null;
  avatarUrl: string | null;
  updatedAt: string | null;
}

// listMembers reads the group's roster (its membership records) and each
// member's profile within the group.
export const listMembers = Effect.fn("listMembers")(function* (
  did: string,
  org: string,
) {
  const memberships = yield* listGroupRecords(
    did,
    org,
    "group.opensocial.membership",
  );
  const profiles = yield* getProfiles(
    did,
    org,
    memberships.map((m) => m.rkey),
  );
  return memberships.map((m): MemberView => {
    const profile = profiles.get(m.rkey);
    return {
      did: m.rkey,
      roles: Array.isArray(m.value.roles) ? (m.value.roles as string[]) : [],
      handle: profile?.handle ?? null,
      displayName: profile?.displayName ?? null,
      avatarUrl: profile?.avatarUrl ?? null,
      updatedAt:
        typeof m.value.createdAt === "string" ? m.value.createdAt : null,
    };
  });
});

export type ProfileView = network.habitat.opensocial.getProfiles.ProfileView;

// getProfiles fetches members' profiles within the group (their
// memberProfile record, else their public profile), 100 at a time. A batch
// that fails is left out: names fall back to DIDs.
export const getProfiles = Effect.fn("getProfiles")(function* (
  did: string,
  org: string,
  dids: readonly string[],
) {
  const habitat = yield* Habitat;
  const profiles = new Map<string, ProfileView>();
  for (let i = 0; i < dids.length; i += 100) {
    const batch = dids.slice(i, i + 100) as DidString[];
    const body = yield* habitat
      .xrpc(
        did,
        network.habitat.opensocial.getProfiles.main,
        { params: { org: org as DidString, dids: batch } },
        viaGroup(org),
      )
      .pipe(
        Effect.tapError((e) => Effect.logWarning("getProfiles failed", e)),
        Effect.option,
      );
    if (Option.isSome(body)) {
      for (const p of body.value.profiles) profiles.set(p.did, p);
    }
  }
  return profiles;
});

export interface RoleView {
  id: string;
  name: string;
  description: string | null;
}

export const listRoles = Effect.fn("listRoles")(function* (
  did: string,
  org: string,
) {
  const records = yield* listGroupRecords(did, org, "group.opensocial.role");
  return records.map((r): RoleView => ({
    id: r.rkey,
    name:
      typeof r.value.displayName === "string" ? r.value.displayName : r.rkey,
    description:
      typeof r.value.description === "string" ? r.value.description : null,
  }));
});

// assignableWith is the assignable list that also lets every role holding
// role.assign assign role, or null when it already does (or nothing holds
// role.assign).
export function assignableWith(
  bindings: readonly { action: string; roles: readonly string[] }[],
  assignable: readonly { role: string; roles: readonly string[] }[],
  role: string,
): { role: string; roles: string[] }[] | null {
  const next = assignable.map((a) => ({ role: a.role, roles: [...a.roles] }));
  const assigners = new Set(
    bindings.filter((b) => b.action === "role.assign").flatMap((b) => b.roles),
  );
  let changed = false;
  for (const assigner of assigners) {
    const entry = next.find((a) => a.role === assigner);
    if (!entry) {
      next.push({ role: assigner, roles: [role] });
      changed = true;
    } else if (!entry.roles.includes(role)) {
      entry.roles.push(role);
      changed = true;
    }
  }
  return changed ? next : null;
}

export type ActionBinding = group.opensocial.permissions.ActionBinding;
export type AssignableBinding = group.opensocial.permissions.AssignableBinding;

export interface PermissionsView {
  // Whether the group has a permissions record at all. Without one, pear
  // lets admins (and only admins) do everything.
  exists: boolean;
  bindings: ActionBinding[];
  assignable: AssignableBinding[];
}

// readPermissions reads the group's permissions record: which roles hold
// which actions, and which roles each role may assign. A group that never
// wrote one has none.
export const readPermissions = Effect.fn("readPermissions")(function* (
  did: string,
  org: string,
) {
  const habitat = yield* Habitat;
  const record = yield* habitat
    .call<{ value?: Partial<PermissionsView> }>(
      did,
      "network.habitat.space.getRecord",
      "GET",
      {
        space: membersSpace(org),
        repo: org,
        collection: "group.opensocial.permissions",
        rkey: "self",
      },
    )
    .pipe(
      Effect.catchTag("HabitatError", (e) =>
        e.status === 404 || e.status === 400
          ? Effect.succeed({ value: undefined })
          : Effect.fail(e),
      ),
    );
  return {
    exists: record.value !== undefined,
    ...permissionsFromRecord(record.value),
  } satisfies PermissionsView;
});

// groupNames resolves display names for groups: the group's profile, else
// the name cached when it was connected. The cache is only a snapshot from
// connect time, so it must not win over the profile or a rename would never
// show.
export const groupNames = Effect.fn("groupNames")(function* (
  did: string,
  orgs: readonly string[],
) {
  const db = yield* Database;
  const cached = yield* Effect.promise(() => connectedOrgNames(db, [...orgs]));
  const names = new Map<string, { name: string | null; connected: boolean }>();
  yield* Effect.forEach(
    orgs,
    (org) =>
      Effect.gen(function* () {
        const profile = yield* readGroupProfile(did, org);
        const fallback = cached.get(org);
        names.set(org, {
          name: profile.name ?? fallback ?? null,
          connected: fallback !== undefined,
        });
      }),
    { concurrency: 8, discard: true },
  );
  return names;
});

// requireConnected fails with OrgNotConnected unless an admin has connected
// the group, i.e. the app holds the group's own OAuth session.
export const requireConnected = Effect.fn("requireConnected")(function* (
  org: string,
) {
  const db = yield* Database;
  const connected = yield* Effect.promise(() => isConnectedOrg(db, org));
  if (!connected) return yield* new OrgNotConnected({ org });
});

// isNotFound reports whether a Habitat failure means the thing asked for
// isn't there.
export function isNotFound(e: HabitatError): boolean {
  return e.status === 404 || e.error === "RecordNotFound";
}
