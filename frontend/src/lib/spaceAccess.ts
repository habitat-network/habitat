// Explains who can read one of a community's spaces. Mirrors the check the
// space host runs (internal/opensocial CheckPermission): a member can read a
// space when at least one of their roles is listed on the space's
// community.opensocial.access record.

export interface SpaceReader {
  did: string;
  // The member's roles that the space's access record lists, i.e. why they
  // can read it. Never empty.
  via: string[];
}

export function explainSpaceReaders(
  accessRoles: readonly string[] | null | undefined,
  members: readonly { did: string; roles: readonly string[] }[],
): SpaceReader[] {
  if (!accessRoles || accessRoles.length === 0) return [];
  const allowed = new Set(accessRoles);
  const readers: SpaceReader[] = [];
  for (const member of members) {
    const via = member.roles.filter((role) => allowed.has(role));
    if (via.length > 0) readers.push({ did: member.did, via });
  }
  return readers;
}
