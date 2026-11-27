# Group-DID OAuth sessions

The org.atgrp spec lets a group act as itself: moderation methods such as
`querySubjects` and `updateSubject` are "authed as the group", and the permission
sets say "for group-DID sessions". This note records how Habitat provides that
and where it stops.

## How a session works

- An app starts an OAuth flow with the group's DID as the login subject.
  `HandleAuthorize` sees the subject is a group and redirects to the group login
  page (`HandleOpensocial`).
- An admin of the group signs in with their own PDS. The token's subject stays
  the group DID, and the token carries an `authorizer` claim naming that admin.
  The authorizer is kept with the authorization code and the refresh token, so a
  refreshed token keeps it.
- On every request, `Store.SessionScopes` resolves the session to the authorizer
  acting with the scopes their roles hold **now**. Demoting or ejecting the
  authorizer narrows every session they approved at once, with no revocation.
- When the token itself asked for permission sets, group methods or space
  scopes, the session is narrowed to those too. A token that asks only for base
  scopes such as `atproto` is bounded by the authorizer alone.
- The group-only methods (`listJoinRequests`, `denyJoinRequest` and the
  `mod.*` queue methods) check the matching `rpc:` scope. `createSpace`,
  `updateSpace` and `deleteSpace` check `space:*?manage=`, and the store applies
  the escalation rule to the authorizer.
- Direct record writes by a session in the meta and members spaces (see
  `Store.AuthorizeGroupWrite`) need the space scope for the collection and
  action, may only change roles in the authorizer's `canAssign` (and add only
  permissions they hold), refuse host-maintained records (the space index and
  published policies), and cannot remove the last admin.

## Not covered

- **Legacy sessions.** A session issued before authorizers were kept has none,
  and acts as the group with every scope until it expires. Re-authorizing the
  app replaces it.
- **Who may authorize.** Only admins may approve a session. The spec lets any
  member, with the session narrowed to their roles, but sessions also reach
  spaces as the group owner, and data-access checks are not yet narrowed by the
  authorizer.
- **`prompt=create`.** Creating a group from the authorize endpoint needs a
  step in the login UI to collect the group's name and handle. Groups are still
  created with `network.habitat.opensocial.createOrg`.
- **Labels.** Label writes in subject spaces are not recorded in moderation
  history, because the generic record path does not guard modality spaces yet.
