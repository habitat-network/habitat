# Email-domain sign-in for opensocial orgs

## Goal

Let users sign in to an opensocial org with a work email (Google OAuth) instead of an existing AT Proto handle. A new endpoint creates an org pre-configured for a given email domain. Nobody is a member yet at creation time; the first person who signs in with a matching email becomes that org's admin, and every subsequent matching sign-in auto-provisions as a plain member. No general onboarding UI is in scope beyond that one creation endpoint.

## Non-goals

- Self-service *management* of an existing domain mapping (changing/removing it) — only creation is in scope.
- Any login method other than Google.
- Per-org Google OAuth app credentials — Habitat brokers a single Google app instance-wide, same as today's `login.googleProvider`.
- Any change to `internal/org` (deprecated) or to lexicon-level record shapes.
- Preventing domain squatting (someone creating an org for a domain they don't control) beyond what sign-in's email-ownership check already rules out — see the "Create-org endpoint" section.

## Data model — new package `internal/emaildomain`

Two gorm-`AutoMigrate`d tables, following the existing no-migration-files convention (see `internal/opensocial/store.go`, `internal/hive/store.go`, etc.):

```go
// domainMapping routes an email domain to the org that owns it and the
// login method members of that domain use.
type domainMapping struct {
    Domain      string     `gorm:"primaryKey"` // e.g. "acme.com"
    OrgDID      syntax.DID `gorm:"not null"`
    LoginMethod string     `gorm:"not null"` // "google" (only value today)
}

// memberEmail records which DID a given work email was provisioned as,
// within its org. Populated once, at first sign-in.
type memberEmail struct {
    Email  string     `gorm:"primaryKey"`
    OrgDID syntax.DID `gorm:"not null"`
    DID    syntax.DID `gorm:"not null;uniqueIndex"`
}
```

`Store` methods:
- `LookupDomain(ctx, domain string) (orgDID syntax.DID, loginMethod string, ok bool, err error)`
- `GetDID(ctx, email string) (did syntax.DID, ok bool, err error)`
- `GetEmail(ctx, did syntax.DID) (email string, ok bool, err error)`
- `GetLoginMethod(ctx, did syntax.DID) (method string, ok bool, err error)` — joins `memberEmail.DID = did` → `memberEmail.OrgDID` → `domainMapping.LoginMethod`. Used by the OAuth callback to decide whether/how to route a DID through this new path.
- `Provision(ctx, email string, orgDID, did syntax.DID) error` — inserts the `memberEmail` row after a successful mint.
- `CreateDomainMapping(ctx, domain string, orgDID syntax.DID, loginMethod string) error` — called by the new create-org endpoint below (see "Create-org endpoint"); no separate standalone management API.

No changes to `internal/opensocial` lexicon-backed record shapes; no new AT Proto record types.

## Org creation without a creator

`opensocial.Store.NewOrg` (`internal/opensocial/store.go:66`) requires a `creator syntax.DID` and writes their admin membership record as part of the same transaction that creates the org's spaces/roles/permissions — there's no creator yet for a domain-mapped org, since nobody's signed in. `NewOrg` is refactored to share its space/role/permissions-record setup (lines 69-193) via a private `createOrgShell(ctx, tx, handle) (*identity.Identity, error)` helper, with a new sibling entry point:

```go
// NewOrgWithoutCreator creates an org's spaces, built-in roles, access, and
// permissions records exactly like NewOrg, but writes no initial membership
// — used when the org will be populated by domain-based auto-provisioning
// (see ProvisionMember) rather than by an existing DID creating it directly.
func (s *Store) NewOrgWithoutCreator(ctx context.Context, handle string) (string, error)
```

## New method: `opensocial.Store.ProvisionMember`

```go
// ProvisionMember adds memberDID to orgDID by writing its membership record
// (authored by the org) and its own acceptance record (authored by the
// member) in a single transaction. Unlike RequestJoin, no prior invite is
// required — used when the caller has independent authority to enroll the
// member (e.g. the email-domain-registry auto-provisioning flow below) and
// there's no invitee session to author the acceptance record under, so the
// backend authors both records directly.
//
// memberDID is granted the admin role if it is the org's first member
// (i.e. the org was created via NewOrgWithoutCreator and nobody has joined
// yet), and the member role otherwise. This check-and-write is made
// race-safe by acquiring the same per-(space, repo) advisory lock PutRecord
// itself takes (spaces.Store.LockRepo, a newly-exported wrapper around the
// existing internal lockRepo helper) before counting existing
// community.opensocial.membership records for orgDID, so two concurrent
// first sign-ins cannot both become admin.
func (s *Store) ProvisionMember(ctx context.Context, orgDID, memberDID syntax.DID) error
```

Implementation mirrors `RequestJoin` (`internal/opensocial/invite.go:146`): within one `s.db.Transaction`, lock the org's members-space repo, count existing `community.opensocial.membership` records for `repo=orgDID` to decide `roles = ["admin"]` or `["member"]`, then write `community.opensocial.membership` (repo=`orgDID`, rkey=`memberDID`) and `community.opensocial.acceptance` (repo=`memberDID`, rkey=`"self"`) into the org's `members` space via `spacesStoreTx.PutRecord`. `PutRecord`'s `repo` argument only selects which DID's record namespace the write lands in — it is not a signature/credential check — so the backend can author the acceptance record under `memberDID` directly, the same way `RequestJoin` already authors the membership record under `orgDID`.

## Create-org endpoint

New lexicon `network.habitat.emaildomain.createOrg` (`{handle, domain}` → `{org: did}`), handled in `internal/pearserver` following the existing `CreateOrg` handler's shape (`internal/pearserver/opensocial_create_org.go:22`): OAuth/service-auth authenticated (any authenticated user, same as `opensocial.createOrg` — no instance-admin gate), validates `handle` the same way, then:
1. `opensocialStore.NewOrgWithoutCreator(ctx, handle)` → `orgDID`.
2. `emailDomainStore.CreateDomainMapping(ctx, domain, orgDID, "google")`.

Any authenticated user can call this — not just instance admins — because claiming a domain mapping grants no access by itself: only someone who can complete Google OAuth for an `@domain` address can ever sign in to the resulting org, and the first such sign-in (not the org creator) becomes admin. The residual risk is domain squatting (claiming `acme.com` before Acme's own admin does, blocking them from registering it later) rather than credential exposure; not addressed here.

## Identity resolution: the email path

`internal/identity` gets a new function, called by both existing resolution entry points when `syntax.ParseAtIdentifier` fails and the input parses as an email address:

```go
func ResolveEmailIdentity(
    ctx context.Context,
    emailStore *emaildomain.Store,
    h hive.Hive,
    opensocialStore *opensocial.Store,
    email string,
) (*identity.Identity, error)
```

Logic:
1. `emailStore.GetDID(email)` — if found, `hive.LookupDID` that DID and return it (returning member, no writes).
2. Else `emailStore.LookupDomain(domain)` — no match → `identity.ErrDIDNotFound` (not our concern; caller's existing "identifier not found" handling applies).
3. Else (first-time signup, domain matches an org):
   a. `hive.MintIdentity(ctx, <generated handle>, <org subdomain>)` → new identity/DID.
   b. `opensocialStore.ProvisionMember(ctx, orgDID, newDID)` — grants admin or member per the first-member rule above.
   c. `emailStore.Provision(ctx, email, orgDID, newDID)`.
   d. Return the new identity.

Call sites:
- `identity.Server.ResolveIdentity` / `ResolveHandle` (`internal/identity/server.go:228`, `:200`) — identifier parsing falls back to `ResolveEmailIdentity` on `ParseAtIdentifier` failure.
- `oauthServer.resolveLoginHint` (`internal/oauthserver/oauth_server.go:355`) — same fallback, so a `login_hint`/`handle` form value that's actually an email resolves to a DID the same way a handle does today, including the mint-on-first-sight behavior. This is the mechanism that turns "user types their work email" into a `did` in the fosite session subject, upstream of any Google verification — matches the already-agreed behavior that provisioning happens inline at resolution time, not gated behind a prior OAuth round-trip.

## OAuth callback: routing through Google

`oauthServer.HandleAuthorize`/`HandleCallback` (`internal/oauthserver/oauth_server.go:216`, `:403`) already branch on `opensocialStore.IsOrg(did)` to divert org-DID logins to PDS OAuth (`HandleOpensocial`), then fall through to `loginRouter.Authorize`/`Exchange` (the existing `org.LoginRouter`, `internal/org/login_router.go`) for everything else. Rather than adding a second, parallel router, this extends `org.LoginRouter` itself with the email-domain check, so `oauthServer`'s call site doesn't change at all.

`org.LoginRouter` gains one new field:

```go
type LoginRouter struct {
    Pds        login.Provider
    Google     login.Provider
    Password   login.Provider
    OrgStore   Store
    EmailStore *emaildomain.Store // new
}
```

`Authorize` and `Exchange` each gain a check at the top, before the existing org/member branching:

```go
func (r *LoginRouter) Authorize(ctx context.Context, did syntax.DID) (string, []byte, error) {
    if method, ok, err := r.EmailStore.GetLoginMethod(ctx, did); err != nil {
        return "", nil, fmt.Errorf("get email login method: %w", err)
    } else if ok {
        provider := r.emailProvider(method) // only "google" today, dispatches to r.Google
        if provider == nil {
            return "", nil, fmt.Errorf("unsupported login provider for %s", did)
        }
        return provider.Authorize(ctx, "")
    }
    // ... existing org/member lookups, unchanged
}
```

`Exchange` follows the same shape as its existing member branch: after `provider.Exchange` returns the OAuth-verified email, compare it against `r.EmailStore.GetEmail(ctx, did)` and reject on mismatch — the same check the member branch already does against `member.LoginID` (`internal/org/login_router.go:102-104`), just sourced from `emaildomain.Store` instead of `org.Store`.

This keeps `internal/org` deprecated but still the single router `oauthServer` depends on; the email-domain check will move out alongside the rest of `LoginRouter` when `internal/org` itself is eventually replaced — not part of this feature's scope.

`org.LoginRouter`'s constructor call in `cmd/pear/main.go` gains one new argument: the `emaildomain.Store` instance.

## Frontend

Out of scope for this spec beyond noting the shape needed: the existing login form's identifier field needs to accept an email as well as a handle (no new page) — `resolveLoginHint` accepting an email transparently means the existing `/oauth-login` → `HandleAuthorize` → `HandleCallback` flow works unchanged once the backend changes land; a small frontend follow-up to accept/label the field as "handle or work email" and to render the Google consent redirect is expected but not detailed here.

## Testing

Per `go-tdd`/`go-conventions`: TDD for `emaildomain.Store`, `opensocial.Store.NewOrgWithoutCreator`/`ProvisionMember`, `identity.ResolveEmailIdentity`, the new `emaildomain.createOrg` handler, and the extended `org.LoginRouter`. Key cases:
- Domain match, first sign-in → mints identity, provisions membership (role `admin`) + acceptance + `memberEmail` row, returns new identity.
- Domain match, second sign-in (different email, same org) → provisions as role `member`.
- Domain match, returning member → no writes, returns existing identity.
- No domain match → `ErrDIDNotFound`, unchanged behavior for the caller.
- Two concurrent first-time sign-ins for the same org race `ProvisionMember`; exactly one becomes admin (covers the `LockRepo` serialization).
- `ProvisionMember` is transactional — a failure writing the acceptance record must roll back the membership write.
- `NewOrgWithoutCreator` creates spaces/roles/permissions identically to `NewOrg` but writes no membership record.
- OAuth callback: Google-verified email mismatched against `memberEmail` → login rejected.
- Two domains can map to the same org; a domain cannot map to two orgs (`Domain` primary key enforces this).
