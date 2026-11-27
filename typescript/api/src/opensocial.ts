/**
 * Hand-written readers for group.opensocial records.
 *
 * The group.opensocial lexicons give roles OAuth scopes (access and
 * permissions grants), while Habitat's UI still works with named actions.
 * Each action stands for the scopes it requires, mirroring ActionScopes in
 * internal/opensocial/actions.go; these helpers read a permissions record's
 * grants back into per-action role bindings. They are for display: the server
 * makes the authorization decisions.
 */

export const READ_SCOPE = 'read'

export type ActionBinding = { action: string; roles: string[] }
export type AssignableBinding = { role: string; roles: string[] }

type Grant = { role?: unknown; scopes?: unknown; assignable?: unknown }

function strings(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((v): v is string => typeof v === 'string')
    : []
}

function grants(value: unknown): Grant[] {
  const list = (value as { grants?: unknown } | undefined)?.grants
  return Array.isArray(list) ? (list as Grant[]) : []
}

/** Roles that hold the read scope in a group.opensocial.access record value. */
export function accessReadRoles(value: unknown): string[] {
  return grants(value)
    .filter((g) => strings(g.scopes).includes(READ_SCOPE))
    .map((g) => g.role)
    .filter((r): r is string => typeof r === 'string')
}

/** Builds access grants giving each role the read scope. */
export function readGrants(roles: string[]) {
  return roles.map((role) => ({ role, scopes: [READ_SCOPE] }))
}

const MEMBERS = 'group.opensocial.members'
const INVITES = 'group.opensocial.invites'
const MEMBERSHIP = 'collection=group.opensocial.membership'

/** The scopes each action requires (internal/opensocial/actions.go). */
export const ACTION_SCOPES: Record<string, string[]> = {
  invite: [
    `space:${INVITES}?authority=*&skey=self&collection=group.opensocial.invite&action=create`,
  ],
  eject: [`space:${MEMBERS}?authority=self&skey=self&${MEMBERSHIP}&action=delete`],
  'role.assign': [
    `space:${MEMBERS}?authority=self&skey=self&${MEMBERSHIP}&action=create&action=update`,
  ],
  'space.create': ['space:*?manage=create'],
  'space.delete': ['space:*?manage=delete'],
  'space.configure': ['include:group.opensocial.manageSpaces'],
  'community.configure': ['include:group.opensocial.configure'],
  'mcp.configure': ['habitat:mcp=configure'],
}

// Broader scopes that also grant a required scope.
const MANAGE_BOTH = 'space:*?manage=create&manage=delete'
const IMPLIED_BY: Record<string, string[]> = {
  [ACTION_SCOPES.invite[0]]: ['include:group.opensocial.manageMembers'],
  [ACTION_SCOPES.eject[0]]: ['include:group.opensocial.manageMembers'],
  [ACTION_SCOPES['role.assign'][0]]: ['include:group.opensocial.manageMembers'],
  'space:*?manage=create': [MANAGE_BOTH],
  'space:*?manage=delete': [MANAGE_BOTH],
}

function holds(scopes: string[], required: string): boolean {
  return (
    scopes.includes(required) ||
    (IMPLIED_BY[required] ?? []).some((s) => scopes.includes(s))
  )
}

/** Action bindings and assignable roles from a permissions record value. */
export function permissionsFromRecord(value: unknown): {
  bindings: ActionBinding[]
  assignable: AssignableBinding[]
} {
  const list = grants(value)
  const bindings: ActionBinding[] = []
  for (const [action, required] of Object.entries(ACTION_SCOPES)) {
    const roles = list
      .filter(
        (g) =>
          typeof g.role === 'string' &&
          required.every((r) => holds(strings(g.scopes), r)),
      )
      .map((g) => g.role as string)
    if (roles.length > 0) bindings.push({ action, roles })
  }
  const assignable: AssignableBinding[] = []
  for (const grant of list) {
    const roles = strings(grant.assignable)
    if (typeof grant.role === 'string' && roles.length > 0) {
      assignable.push({ role: grant.role, roles })
    }
  }
  return { bindings, assignable }
}
