# Group-DID OAuth sessions

The opensocial spec lets a group act as itself: moderation methods such as
`listSubjects` and `resolveSubject` are "authed as the group", and the
permission sets say "For group-DID sessions". This note records how Habitat
provides that and where it stops.

## What exists

- An app starts an OAuth flow with the group's DID as the login subject.
  `HandleAuthorize` sees the subject is a group and redirects to the opensocial
  login page (`HandleOpensocial`).
- An admin member of the group signs in with their own PDS. The token's subject
  stays the group DID, so the app holds a session that acts as the group.
- Pear's group handlers accept that credential (`groupCaller`) and treat the
  group DID as a member holding every scope.

## What this change adds

1. **Granting is capped by the approving admin.** `HandleOpensocial` refuses the
   flow when a requested scope (`include:`, `space:`, `rpc:`, `habitat:`) is
   not covered by the admin's own scopes. This is the spec's escalation rule
   applied to OAuth: a session for the group can't hold more than the person who
   approved it.
2. **The token's scopes bound the session.** The group DID holds every scope,
   but an OAuth session for it holds only what the token was granted.
   `requireGroupRPC` checks the granted scopes for the method (`rpc:<lxm>`), so
   an app granted only `include:group.opensocial.moderate` can't call
   `denyJoinRequest`.

## Not covered yet

- Only the `rpc:` checks in `requireGroupRPC` use the token's scopes.
  `createSpace`, `deleteSpace` and the record-write paths still treat a group
  session as holding every scope; they should check the granted `space:` scopes.
- A session is not bound to the admin after the flow ends. If the admin loses
  their role, outstanding tokens keep working until they expire or are revoked.
  Re-checking the approving admin on refresh is the next step.
- Consent shows the requested scopes as OAuth strings. A friendlier display
  should name the permission sets (for example "Group moderation").
- Whether a group-DID session may be used by software the group's admins did
  not install is a policy question the spec leaves open.
