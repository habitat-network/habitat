/**
 * Hand-written readers for group.opensocial records.
 *
 * The group.opensocial lexicons give roles OAuth scopes (access and
 * permissions grants), while Habitat still authorizes by named action. Until
 * scope-based authorization lands, an action is stored as the scope
 * `habitat:action=<action>` (the Go side of this bridge is
 * internal/opensocial/grants.go), and these helpers translate the records
 * back into the shapes the apps work with.
 */

export const READ_SCOPE = 'read'
export const HABITAT_ACTION_SCOPE_PREFIX = 'habitat:action='

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

/** Action bindings and assignable roles from a permissions record value. */
export function permissionsFromRecord(value: unknown): {
  bindings: ActionBinding[]
  assignable: AssignableBinding[]
} {
  const byAction = new Map<string, string[]>()
  const assignable: AssignableBinding[] = []
  for (const grant of grants(value)) {
    if (typeof grant.role !== 'string') continue
    for (const scope of strings(grant.scopes)) {
      if (!scope.startsWith(HABITAT_ACTION_SCOPE_PREFIX)) continue
      const action = scope.slice(HABITAT_ACTION_SCOPE_PREFIX.length)
      byAction.set(action, [...(byAction.get(action) ?? []), grant.role])
    }
    const roles = strings(grant.assignable)
    if (roles.length > 0) assignable.push({ role: grant.role, roles })
  }
  return {
    bindings: [...byAction].map(([action, roles]) => ({ action, roles })),
    assignable,
  }
}
