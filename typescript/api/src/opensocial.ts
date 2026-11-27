/**
 * Hand-written readers for org.atgrp records.
 *
 * The org.atgrp lexicons give roles OAuth-style scopes (a role record's
 * `permissions`) and a `canAssign` list, while Habitat's UI still works with
 * named actions. Each action stands for the scopes it requires, mirroring
 * ActionScopes in internal/opensocial/actions.go; these helpers read role
 * records back into per-action role bindings. They are for display: the
 * server makes the authorization decisions.
 */

const MEMBERS = 'org.atgrp.members'
const INBOX = 'org.atgrp.inbox'
const MEMBERSHIP = 'collection=org.atgrp.membership'

/** The prefix of a scope that stands for one Habitat action. */
export const ACTION_SCOPE_PREFIX = 'habitat:action='

/** The canAssign entry that covers every role. */
export const ASSIGN_ALL = '*'

export type ActionBinding = { action: string; roles: string[] }
export type AssignableBinding = { role: string; roles: string[] }

/** A role record with the key it is stored under. */
export type RoleRecord = { rkey: string; value: unknown }

/** The scopes each action requires (internal/opensocial/actions.go). */
export const ACTION_SCOPES: Record<string, string[]> = {
  invite: [
    `space:${INBOX}?authority=*&skey=self&collection=org.atgrp.invite&action=create`,
  ],
  eject: [`space:${MEMBERS}?authority=self&skey=self&${MEMBERSHIP}&action=delete`],
  'role.assign': [
    `space:${MEMBERS}?authority=self&skey=self&${MEMBERSHIP}&action=create&action=update`,
  ],
  'space.create': ['space:*?manage=create'],
  'space.delete': ['space:*?manage=delete'],
  'space.configure': ['space:*?manage=update'],
  'community.configure': ['include:org.atgrp.authManageAdmin'],
  'mcp.configure': ['habitat:mcp=configure'],
}

// Permission sets that also grant the actions' required scopes.
const SET_GRANTS: Record<string, string[]> = {
  'org.atgrp.authManageMembers': ['invite', 'eject', 'role.assign'],
  'org.atgrp.authManageAdmin': ['community.configure'],
}

/** The manage verbs a `space:*?manage=...` scope grants. */
function manageVerbs(scope: string): string[] {
  if (!scope.startsWith('space:*?')) return []
  return new URLSearchParams(scope.slice('space:*?'.length)).getAll('manage')
}

function strings(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((v): v is string => typeof v === 'string')
    : []
}

/** Whether `scopes` grant `action`. */
export function holdsAction(scopes: string[], action: string): boolean {
  if (scopes.includes(`${ACTION_SCOPE_PREFIX}${action}`)) return true
  const required = ACTION_SCOPES[action] ?? []
  if (required.length === 0) return false
  return required.every((req) => {
    if (scopes.includes(req)) return true
    const verbs = manageVerbs(req)
    if (verbs.length > 0) {
      return scopes.some((s) => {
        const held = manageVerbs(s)
        return verbs.every((v) => held.includes(v))
      })
    }
    return scopes.some((s) => {
      const set = s.startsWith('include:') ? s.slice('include:'.length) : ''
      return (SET_GRANTS[set] ?? []).includes(action)
    })
  })
}

/** Action bindings and assignable roles from the group's role records. */
export function permissionsFromRoles(roles: RoleRecord[]): {
  bindings: ActionBinding[]
  assignable: AssignableBinding[]
} {
  const bindings: ActionBinding[] = []
  for (const action of Object.keys(ACTION_SCOPES)) {
    const holders = roles
      .filter((r) =>
        holdsAction(
          strings((r.value as { permissions?: unknown } | undefined)?.permissions),
          action,
        ),
      )
      .map((r) => r.rkey)
    if (holders.length > 0) bindings.push({ action, roles: holders })
  }
  const assignable: AssignableBinding[] = []
  for (const role of roles) {
    const assign = strings((role.value as { canAssign?: unknown } | undefined)?.canAssign)
    const expanded = assign.includes(ASSIGN_ALL)
      ? roles.map((r) => r.rkey)
      : assign
    if (expanded.length > 0) assignable.push({ role: role.rkey, roles: expanded })
  }
  return { bindings, assignable }
}

/** Roles granted read in an org.atgrp.policy value. */
export function policyReadRoles(value: unknown): string[] {
  const grants = (value as { grants?: unknown } | undefined)?.grants
  if (!Array.isArray(grants)) return []
  return grants
    .filter((g) => g?.read === true && typeof g.role === 'string')
    .map((g) => g.role as string)
}

/** Builds policy grants giving each role read and write. */
export function readWriteGrants(roles: string[]) {
  return roles.map((role) => ({ role, read: true, write: true }))
}
