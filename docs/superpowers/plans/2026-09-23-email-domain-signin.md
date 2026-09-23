# Email-domain sign-in Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user sign in to an opensocial org with a Google-verified work email. A new `network.habitat.emaildomain.createOrg` endpoint creates an org mapped to an email domain. The first matching sign-in becomes admin, and later ones become members.

**Architecture:** A new `internal/emaildomain` package stores domain→org mappings and email→DID provisioning rows. `internal/identity` gains an `EmailResolver` that resolves an email to a DID, minting the identity and enrolling it through a new `opensocial.Store.ProvisionMember` on first sight. `identity.Server` (resolveIdentity/resolveHandle) and `oauthserver.resolveLoginHint` fall back to it when an identifier isn't a handle or DID. `org.LoginRouter` routes email-provisioned DIDs through Google and checks the verified email against the provisioned one.

**Tech Stack:** Go 1.26, gorm (AutoMigrate, SQLite in tests / Postgres in prod), indigo `atproto/identity` + `atproto/syntax`, testify `require`, Moon (`moon :generate`) for lexicon codegen.

**Spec:** `docs/superpowers/specs/2026-09-23-email-domain-signin-design.md`

## Global Constraints

- Go 1.26; entrypoints in `cmd/`, everything else in `internal/`.
- Tables are gorm-`AutoMigrate`d in the store constructor, following the existing no-migration-files convention (`internal/opensocial/store.go`, `internal/hive/store.go`).
- No new AT Proto record types and no changes to `internal/opensocial` lexicon-backed record shapes.
- The only login method is Google (`"google"`). Habitat brokers a single instance-wide Google app (`login.googleProvider`), and there are no per-org credentials.
- `internal/org` is deprecated. The only change allowed there is extending `org.LoginRouter` with the `EmailStore` field and email branch. No other `internal/org` changes.
- Don't hand-edit generated code (`api/habitat`, `typescript/api`, the openapi spec, `api-docs`). Run `moon :generate`.
- Tests use `testing` + `require` only, with interface-based fakes and no mocks. Never `time.Sleep`. Name tests `TestComponentWhatItTests`.
- Never remove existing comments. When changing a signature that has many call sites, use a sed/perl script rather than hand-editing each one.
- Use typedefs instead of raw strings for domain values (`emaildomain.Email`, `emaildomain.LoginMethod`).
- Handle or return an error, never both. Wrap errors with `fmt.Errorf("...: %w", err)`, and define sentinels with `errors.New` where callers branch on them.

## Deviations from the spec (deliberate; flag in review if unwanted)

1. **`ResolveEmailIdentity` is a method on `identity.EmailResolver`** instead of a free function that takes three store parameters. Both `identity.Server` and `oauthserver.OAuthServer` need the same logic injected, so it's a single dependency (`oauthserver` sees it through a small `EmailIdentityResolver` interface, which keeps `oauthserver` from importing `internal/identity`).
2. **First-sign-in steps a–c run in one DB transaction.** This uses the new `opensocial.Store.WithTx`, `emaildomain.Store.WithTx`/`Transaction`, and the existing `hive.WithTx`. A failure part-way through, or two simultaneous first sign-ins with the *same* email, can then no longer leave a ghost member or a dangling identity. On a duplicate-email race the loser re-reads and returns the winner's DID.
3. **Google `Authorize` gets the provisioned email as `login_hint`**, not `""`, so Google preselects the right account. `Exchange` still verifies it.
4. **Emails and domains are lowercased** at parse/store time. Google-returned emails are compared case-insensitively.
5. **`createOrg` rejects a domain that's already mapped with 409 `DomainTaken` before minting the org**, so a rejected call doesn't leave an orphan org in the common case.
6. **`oauthServer.resolveLoginHint` takes a `ctx`.** It now performs writes, so it shouldn't run on `context.Background()`.

## Review Focus

1. **Mixed-case email**: a user types `Alice@ACME.com`, Google returns `alice@acme.com`, and an admin created the mapping as `ACME.com`. It must resolve to one DID and log in. Pinned in Task 1 (`ParseEmail` lowercases, mapping lowercases), Task 4 (returning member with different casing), Task 5 (`EqualFold` on exchange), and Task 7 (`ACME.com` domain on create).
2. **The same email submitted twice concurrently** (double-click, or PAR + authorize): the result must be exactly one identity and one membership, and both calls return the same DID. Pinned in Task 4 (`TestEmailResolverConcurrentSameEmail`).
3. **Two emails whose local parts sanitize to the same handle** (`alice@`, `a.lice@`): both must sign up, with distinct DIDs. Pinned in Task 4 (`TestEmailResolverHandleCollision`).
4. **Things that look almost like emails** (`Alice <alice@acme.com>`, `alice@localhost`, `@acme.com`, plain handles, DIDs): these must never mint. They keep today's "invalid identifier"/handle behavior. Pinned in Task 1 (`TestParseEmail`) and Task 4 (`TestResolveIdentityEmailDisabled`).
5. **An instance without Google configured**, where an email member tries to log in: it must return a clear error, not a nil-pointer panic. Pinned in Task 5 (`google not configured` subtest).

## Known risks carried over from the spec (not addressed by this plan)

- Provisioning happens at *resolution* time, before any Google verification (spec, "Identity resolution"). So anyone who types `anything@acme.com` into the login form, or hits the public `resolveIdentity`/`resolveHandle` XRPC, mints an identity and membership. The first such unverified email becomes the org's admin, even if nobody owns that address.
- Mapping a public-mail domain (e.g. `gmail.com`) is not prevented.
- A lost race between two concurrent `createOrg` calls for the same domain still leaves one orphan org (the 409 pre-check narrows but doesn't close this window).

---

## File Structure

| File | Responsibility |
|---|---|
| Create `internal/emaildomain/email.go` | `Email` / `LoginMethod` types, `ParseEmail`, `Domain()` / `LocalPart()` |
| Create `internal/emaildomain/store.go` | gorm models `domainMapping`, `memberEmail`; `Store` with lookup/provision/mapping methods, `WithTx`, `Transaction` |
| Create `internal/emaildomain/email_test.go`, `store_test.go` | tests |
| Modify `internal/spaces/store.go` | export `LockRepo` on the `Store` interface |
| Create `internal/spaces/lock_repo_test.go` | test |
| Modify `internal/opensocial/store.go` | `createOrgShell` refactor, `NewOrgWithoutCreator`, `putMembership`, `WithTx` |
| Create `internal/opensocial/provision.go` | `ProvisionMember` |
| Create `internal/opensocial/provision_test.go`; modify `store_test.go` | tests |
| Create `internal/identity/email.go` | `EmailResolver`, handle generation |
| Modify `internal/identity/server.go` | `WithEmailResolver` option, email fallback in `ResolveIdentity` / `ResolveHandle` |
| Create `internal/identity/email_test.go` | resolver + server tests |
| Modify `internal/org/login_router.go`; `login_router_test.go` | `EmailStore` field + email branch |
| Modify `internal/oauthserver/oauth_server.go`; `oauth_server_test.go` | `EmailIdentityResolver`, `resolveLoginHint(ctx, …)` fallback, new ctor param |
| Create `internal/oauthserver/login_hint_test.go` | test |
| Create `lexicons/network/habitat/emaildomain/createOrg.json` | lexicon (then `moon :generate`) |
| Create `internal/pearserver/emaildomain_create_org.go`, `emaildomain_create_org_test.go` | handler + tests |
| Modify `internal/pearserver/server.go`, `internal/pearserver/testutil/server.go` | `emailDomainStore` dependency |
| Modify `cmd/pear/main.go` | wiring + route |
| Modify `CLAUDE.md` | list the new `emaildomain` package |

---

### Task 1: `internal/emaildomain` package

**Files:**
- Create: `internal/emaildomain/email.go`
- Create: `internal/emaildomain/store.go`
- Test: `internal/emaildomain/email_test.go`, `internal/emaildomain/store_test.go`

**Interfaces:**
- Consumes: `db_testutil.NewDB(t) *gorm.DB` (`internal/db/testutil`). Its SQLite DSN uses `_txlock=immediate` and `TranslateError: true`, so duplicate keys surface as `gorm.ErrDuplicatedKey`.
- Produces:
  - `type Email string`; `func ParseEmail(s string) (Email, error)`; `func (e Email) Domain() string`; `func (e Email) LocalPart() string`; `var ErrInvalidEmail`
  - `type LoginMethod string`; `const LoginMethodGoogle LoginMethod = "google"`
  - `var ErrDomainTaken`, `var ErrEmailProvisioned`
  - `func NewStore(db *gorm.DB) (*Store, error)`
  - `func (s *Store) WithTx(tx *gorm.DB) *Store`
  - `func (s *Store) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error`
  - `func (s *Store) LookupDomain(ctx context.Context, domain string) (syntax.DID, LoginMethod, bool, error)`
  - `func (s *Store) GetDID(ctx context.Context, email Email) (syntax.DID, bool, error)`
  - `func (s *Store) GetEmail(ctx context.Context, did syntax.DID) (Email, bool, error)`
  - `func (s *Store) GetLoginMethod(ctx context.Context, did syntax.DID) (LoginMethod, bool, error)`
  - `func (s *Store) Provision(ctx context.Context, email Email, orgDID, did syntax.DID) error`
  - `func (s *Store) CreateDomainMapping(ctx context.Context, domain string, orgDID syntax.DID, method LoginMethod) error`

- [ ] **Step 1: Write the failing email-parsing test**

`internal/emaildomain/email_test.go`:

```go
package emaildomain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/emaildomain"
)

func TestParseEmail(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    emaildomain.Email
		wantErr bool
	}{
		{name: "plain", in: "alice@acme.com", want: "alice@acme.com"},
		{name: "lowercases", in: "Alice@ACME.com", want: "alice@acme.com"},
		{name: "subdomain", in: "bob@eng.acme.co.uk", want: "bob@eng.acme.co.uk"},
		{name: "display name", in: "Alice <alice@acme.com>", wantErr: true},
		{name: "no local part", in: "@acme.com", wantErr: true},
		{name: "no domain", in: "alice@", wantErr: true},
		{name: "single-label domain", in: "alice@localhost", wantErr: true},
		{name: "handle", in: "alice.acme.com", wantErr: true},
		{name: "did", in: "did:web:acme.com", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := emaildomain.ParseEmail(tt.in)
			if tt.wantErr {
				require.ErrorIs(t, err, emaildomain.ErrInvalidEmail)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEmailParts(t *testing.T) {
	email, err := emaildomain.ParseEmail("alice@acme.com")
	require.NoError(t, err)
	require.Equal(t, "alice", email.LocalPart())
	require.Equal(t, "acme.com", email.Domain())
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test ./internal/emaildomain/...`
Expected: FAIL (build error: package `emaildomain` does not exist / undefined `ParseEmail`)

- [ ] **Step 3: Implement `email.go`**

`internal/emaildomain/email.go`:

```go
// Package emaildomain maps email domains to the opensocial orgs that own
// them, and records which DID each work email was provisioned as, backing
// email-based sign-in (see identity.EmailResolver).
package emaildomain

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

var ErrInvalidEmail = errors.New("invalid email address")

// Email is a bare, lowercased email address, e.g. "alice@acme.com".
type Email string

// LoginMethod is how members of an email domain prove they own their email.
type LoginMethod string

const LoginMethodGoogle LoginMethod = "google"

// ParseEmail validates s as a bare email address ("local@domain", with no
// display name or angle brackets) whose domain is a valid DNS name, and
// lowercases it so lookups are case-insensitive.
func ParseEmail(s string) (Email, error) {
	lower := strings.ToLower(s)
	addr, err := mail.ParseAddress(lower)
	if err != nil || addr.Name != "" || addr.Address != lower {
		return "", fmt.Errorf("%w: %q", ErrInvalidEmail, s)
	}
	// Handle syntax is DNS-name syntax, and also rejects single-label hosts
	// like "localhost".
	if _, err := syntax.ParseHandle(lower[strings.LastIndex(lower, "@")+1:]); err != nil {
		return "", fmt.Errorf("%w: %q: invalid domain", ErrInvalidEmail, s)
	}
	return Email(lower), nil
}

// Domain returns the part after the last "@", e.g. "acme.com".
func (e Email) Domain() string {
	return string(e[strings.LastIndex(string(e), "@")+1:])
}

// LocalPart returns the part before the last "@", e.g. "alice".
func (e Email) LocalPart() string {
	return string(e[:strings.LastIndex(string(e), "@")])
}
```

- [ ] **Step 4: Run the email tests and confirm they pass**

Run: `go test ./internal/emaildomain/... -run 'TestParseEmail|TestEmailParts' -v`
Expected: PASS

- [ ] **Step 5: Write the failing store test**

`internal/emaildomain/store_test.go`:

```go
package emaildomain_test

import (
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/emaildomain"
)

// TestStore runs against one store shared by the subtests, in order: the
// member-email subtests rely on the mapping created by "domain mapping".
func TestStore(t *testing.T) {
	s, err := emaildomain.NewStore(db_testutil.NewDB(t))
	require.NoError(t, err)
	org := syntax.DID("did:web:acme.example.com")
	alice := syntax.DID("did:web:alice.example.com")
	aliceEmail := emaildomain.Email("alice@acme.com")

	t.Run("domain mapping", func(t *testing.T) {
		// Domains are stored lowercased.
		require.NoError(t, s.CreateDomainMapping(
			t.Context(), "ACME.com", org, emaildomain.LoginMethodGoogle,
		))
		gotOrg, method, ok, err := s.LookupDomain(t.Context(), "acme.com")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, org, gotOrg)
		require.Equal(t, emaildomain.LoginMethodGoogle, method)

		// Two domains may map to the same org...
		require.NoError(t, s.CreateDomainMapping(
			t.Context(), "acme.io", org, emaildomain.LoginMethodGoogle,
		))
		// ...but one domain can't map to two orgs.
		err = s.CreateDomainMapping(
			t.Context(), "acme.com", "did:web:other.example.com", emaildomain.LoginMethodGoogle,
		)
		require.ErrorIs(t, err, emaildomain.ErrDomainTaken)

		_, _, ok, err = s.LookupDomain(t.Context(), "unknown.com")
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("member emails", func(t *testing.T) {
		_, ok, err := s.GetDID(t.Context(), aliceEmail)
		require.NoError(t, err)
		require.False(t, ok)
		_, ok, err = s.GetLoginMethod(t.Context(), alice)
		require.NoError(t, err)
		require.False(t, ok)

		require.NoError(t, s.Provision(t.Context(), aliceEmail, org, alice))

		did, ok, err := s.GetDID(t.Context(), aliceEmail)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, alice, did)

		email, ok, err := s.GetEmail(t.Context(), alice)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, aliceEmail, email)

		method, ok, err := s.GetLoginMethod(t.Context(), alice)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, emaildomain.LoginMethodGoogle, method)

		err = s.Provision(t.Context(), aliceEmail, org, "did:web:alice2.example.com")
		require.ErrorIs(t, err, emaildomain.ErrEmailProvisioned)
	})

	t.Run("transaction rolls back", func(t *testing.T) {
		bobEmail := emaildomain.Email("bob@acme.com")
		err := s.Transaction(t.Context(), func(tx *gorm.DB) error {
			require.NoError(t, s.WithTx(tx).Provision(
				t.Context(), bobEmail, org, "did:web:bob.example.com",
			))
			return errors.New("roll back")
		})
		require.Error(t, err)
		_, ok, err := s.GetDID(t.Context(), bobEmail)
		require.NoError(t, err)
		require.False(t, ok)
	})
}
```

- [ ] **Step 6: Run it and confirm it fails**

Run: `go test ./internal/emaildomain/... -run TestStore`
Expected: FAIL (build error: undefined `emaildomain.NewStore`)

- [ ] **Step 7: Implement `store.go`**

`internal/emaildomain/store.go`:

```go
package emaildomain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
)

var (
	ErrDomainTaken      = errors.New("email domain is already mapped to an org")
	ErrEmailProvisioned = errors.New("email is already provisioned")
)

// domainMapping routes an email domain to the org that owns it and the
// login method members of that domain use.
type domainMapping struct {
	Domain      string      `gorm:"primaryKey"` // e.g. "acme.com"
	OrgDID      syntax.DID  `gorm:"not null"`
	LoginMethod LoginMethod `gorm:"not null"` // "google" (only value today)
}

// memberEmail records which DID a given work email was provisioned as,
// within its org. Populated once, at first sign-in.
type memberEmail struct {
	Email  Email      `gorm:"primaryKey"`
	OrgDID syntax.DID `gorm:"not null"`
	DID    syntax.DID `gorm:"not null;uniqueIndex"`
}

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) (*Store, error) {
	if err := db.AutoMigrate(&domainMapping{}, &memberEmail{}); err != nil {
		return nil, fmt.Errorf("automigrate: %w", err)
	}
	return &Store{db: db}, nil
}

// WithTx returns a copy of the store whose operations run on tx.
func (s *Store) WithTx(tx *gorm.DB) *Store {
	return &Store{db: tx}
}

// Transaction runs fn in a transaction on this store's DB, so callers can
// scope this and other stores (via their WithTx) to the same transaction.
func (s *Store) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return s.db.WithContext(ctx).Transaction(fn)
}

func (s *Store) LookupDomain(
	ctx context.Context,
	domain string,
) (syntax.DID, LoginMethod, bool, error) {
	var row domainMapping
	err := s.db.WithContext(ctx).
		Where("domain = ?", strings.ToLower(domain)).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("lookup domain: %w", err)
	}
	return row.OrgDID, row.LoginMethod, true, nil
}

func (s *Store) GetDID(ctx context.Context, email Email) (syntax.DID, bool, error) {
	var row memberEmail
	err := s.db.WithContext(ctx).Where("email = ?", email).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get did by email: %w", err)
	}
	return row.DID, true, nil
}

func (s *Store) GetEmail(ctx context.Context, did syntax.DID) (Email, bool, error) {
	var row memberEmail
	err := s.db.WithContext(ctx).Where("did = ?", did).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get email by did: %w", err)
	}
	return row.Email, true, nil
}

// GetLoginMethod returns the login method of the org did was provisioned
// into via email sign-in; ok is false for any DID not provisioned that way.
// If the org has several mapped domains, any one's method is returned —
// they're all "google" today.
func (s *Store) GetLoginMethod(
	ctx context.Context,
	did syntax.DID,
) (LoginMethod, bool, error) {
	var methods []LoginMethod
	if err := s.db.WithContext(ctx).
		Model(&memberEmail{}).
		Joins("JOIN domain_mappings ON domain_mappings.org_did = member_emails.org_did").
		Where("member_emails.did = ?", did).
		Limit(1).
		Pluck("domain_mappings.login_method", &methods).Error; err != nil {
		return "", false, fmt.Errorf("get login method: %w", err)
	}
	if len(methods) == 0 {
		return "", false, nil
	}
	return methods[0], true, nil
}

// Provision records that email was provisioned as did in orgDID. It returns
// ErrEmailProvisioned if email already has a DID.
func (s *Store) Provision(
	ctx context.Context,
	email Email,
	orgDID, did syntax.DID,
) error {
	err := s.db.WithContext(ctx).Create(&memberEmail{
		Email:  email,
		OrgDID: orgDID,
		DID:    did,
	}).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrEmailProvisioned
	}
	if err != nil {
		return fmt.Errorf("provision email: %w", err)
	}
	return nil
}

// CreateDomainMapping maps domain to orgDID. It returns ErrDomainTaken if
// domain is already mapped to an org.
func (s *Store) CreateDomainMapping(
	ctx context.Context,
	domain string,
	orgDID syntax.DID,
	method LoginMethod,
) error {
	err := s.db.WithContext(ctx).Create(&domainMapping{
		Domain:      strings.ToLower(domain),
		OrgDID:      orgDID,
		LoginMethod: method,
	}).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrDomainTaken
	}
	if err != nil {
		return fmt.Errorf("create domain mapping: %w", err)
	}
	return nil
}
```

- [ ] **Step 8: Run the package tests and confirm they pass**

Run: `go test ./internal/emaildomain/... -v`
Expected: PASS (all of `TestParseEmail`, `TestEmailParts`, `TestStore/*`)

- [ ] **Step 9: Commit**

```bash
git add internal/emaildomain
git commit -m "Add emaildomain store for email-domain sign-in"
```

---

### Task 2: `opensocial.Store.NewOrgWithoutCreator` and `WithTx`

**Files:**
- Modify: `internal/opensocial/store.go:66-200` (`NewOrg`)
- Test: `internal/opensocial/store_test.go` (append)

**Interfaces:**
- Consumes: the existing `hive.Hive.WithTx(tx) Hive` and `spaces.Store.WithTx(tx) spaces.Store`
- Produces:
  - `func (s *Store) NewOrgWithoutCreator(ctx context.Context, handle string) (string, error)`: returns the org DID string, like `NewOrg`
  - `func (s *Store) WithTx(tx *gorm.DB) *Store`
  - unexported `func (s *Store) createOrgShell(ctx context.Context, tx *gorm.DB, handle string) (*identity.Identity, error)` and `func putMembership(ctx context.Context, spacesStore spaces.Store, orgDID, member syntax.DID, roles []string) error`. Task 3 uses `putMembership`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/opensocial/store_test.go` (add `"errors"` and `"gorm.io/gorm"` to its imports):

```go
func TestStoreNewOrgWithoutCreator(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	orgDIDStr, err := s.NewOrgWithoutCreator(t.Context(), "acme")
	require.NoError(t, err)
	org := syntax.DID(orgDIDStr)

	isOrg, err := s.IsOrg(t.Context(), org)
	require.NoError(t, err)
	require.True(t, isOrg)

	// Same bootstrap records as NewOrg...
	aboutSpace := habitat_syntax.ConstructSpaceURI(org, opensocial.AboutSpaceType, "self")
	_, err = s.SpaceStore.GetRecord(t.Context(), aboutSpace, org, opensocial.ProfileCollection, "self")
	require.NoError(t, err)
	membersSpace := habitat_syntax.ConstructSpaceURI(org, opensocial.MembersSpaceType, "self")
	for _, rkey := range []syntax.RecordKey{opensocial.AdminRoleRkey, opensocial.MemberRoleRkey} {
		_, err = s.SpaceStore.GetRecord(t.Context(), membersSpace, org, "community.opensocial.role", rkey)
		require.NoError(t, err)
	}
	_, err = s.SpaceStore.GetRecord(t.Context(), membersSpace, org, opensocial.PermissionsCollection, "self")
	require.NoError(t, err)

	// ...but nobody is a member yet.
	membershipNSID := syntax.NSID(opensocial.MembershipCollection)
	memberships, err := s.SpaceStore.ListRecords(t.Context(), membersSpace, org, &membershipNSID)
	require.NoError(t, err)
	require.Empty(t, memberships)
}

func TestStoreWithTxRollsBack(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	var orgDIDStr string
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		orgDIDStr, err = s.WithTx(tx).NewOrgWithoutCreator(t.Context(), "acme")
		require.NoError(t, err)
		return errors.New("roll back")
	})
	require.Error(t, err)

	isOrg, err := s.IsOrg(t.Context(), syntax.DID(orgDIDStr))
	require.NoError(t, err)
	require.False(t, isOrg)
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./internal/opensocial/... -run 'TestStoreNewOrgWithoutCreator|TestStoreWithTxRollsBack'`
Expected: FAIL (build error: `s.NewOrgWithoutCreator` / `s.WithTx` undefined)

- [ ] **Step 3: Refactor `NewOrg` into `createOrgShell` + `putMembership`, and add `NewOrgWithoutCreator` and `WithTx`**

In `internal/opensocial/store.go`, add `"github.com/bluesky-social/indigo/atproto/identity"` to the imports. Then replace the whole `NewOrg` function (currently lines 66–200) with the code below.

The body of `createOrgShell` is the **current lines 69–193 moved verbatim**, including the comment block above the permissions record. There are exactly three edits to that moved code:

- (a) Delete the membership block (current lines 126–139: the `CommunityOpensocialMembership` marshal and its `PutRecord`).
- (b) Change each `return fmt.Errorf(...)` to `return nil, fmt.Errorf(...)`.
- (c) Replace the final `orgDID = orgID.DID; return nil` with `return orgID, nil`.

```go
func (s *Store) NewOrg(ctx context.Context, handle string, creator syntax.DID) (string, error) {
	var orgDID syntax.DID
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		orgID, err := s.createOrgShell(ctx, tx, handle)
		if err != nil {
			return err
		}
		if err := putMembership(
			ctx, s.spacesStore.WithTx(tx), orgID.DID, creator, []string{AdminRoleRkey},
		); err != nil {
			return err
		}
		orgDID = orgID.DID
		return nil
	}); err != nil {
		return "", fmt.Errorf("new org: %w", err)
	}
	return orgDID.String(), nil
}

// NewOrgWithoutCreator creates an org's spaces, built-in roles, access, and
// permissions records exactly like NewOrg, but writes no initial membership
// — used when the org will be populated by domain-based auto-provisioning
// (see ProvisionMember) rather than by an existing DID creating it directly.
func (s *Store) NewOrgWithoutCreator(ctx context.Context, handle string) (string, error) {
	var orgDID syntax.DID
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		orgID, err := s.createOrgShell(ctx, tx, handle)
		if err != nil {
			return err
		}
		orgDID = orgID.DID
		return nil
	}); err != nil {
		return "", fmt.Errorf("new org without creator: %w", err)
	}
	return orgDID.String(), nil
}

// WithTx returns a copy of the store whose writes run on tx, so callers can
// compose its operations with other stores' writes in one transaction.
func (s *Store) WithTx(tx *gorm.DB) *Store {
	return &Store{
		db:          tx,
		spacesStore: s.spacesStore.WithTx(tx),
		blobStore:   s.blobStore,
		hive:        s.hive.WithTx(tx),
	}
}

// createOrgShell mints the org identity and writes its about/members spaces,
// profile, built-in roles, access, and permissions records within tx — every
// part of org creation except its initial membership.
func (s *Store) createOrgShell(
	ctx context.Context,
	tx *gorm.DB,
	handle string,
) (*identity.Identity, error) {
	orgID, err := s.hive.WithTx(tx).MintOrgIdentity(ctx, handle)
	if err != nil {
		return nil, fmt.Errorf("mint org identity: %w", err)
	}
	spacesStoreTx := s.spacesStore.WithTx(tx)
	// ... current lines 74–125 and 140–193, moved verbatim with edits (a)–(c) above ...
	return orgID, nil
}

// putMembership writes member's community.opensocial.membership record,
// authored by the org, into the org's members space.
func putMembership(
	ctx context.Context,
	spacesStore spaces.Store,
	orgDID, member syntax.DID,
	roles []string,
) error {
	recordBytes, err := spaces.MarshalRecord(opensocial_api.CommunityOpensocialMembership{
		Roles:     roles,
		UpdatedAt: time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal membership record: %w", err)
	}
	if _, _, err = spacesStore.PutRecord(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		orgDID,
		MembershipCollection,
		syntax.RecordKey(member),
		recordBytes,
	); err != nil {
		return fmt.Errorf("put membership record: %w", err)
	}
	return nil
}
```

(The `// ... current lines ...` line above is a pointer for the mover, not code to paste. The moved code is the existing file content, so nothing needs to be invented. Its first two statements, `MintOrgIdentity` and `spacesStoreTx :=`, are already written out above, which is why the verbatim range starts at line 74.)

- [ ] **Step 4: Run the opensocial tests and confirm they all pass**

Run: `go test ./internal/opensocial/... -v`
Expected: PASS. That includes the existing `TestStore` (which asserts that `NewOrg` still bootstraps the creator as admin) and both new tests.

- [ ] **Step 5: Commit**

```bash
git add internal/opensocial/store.go internal/opensocial/store_test.go
git commit -m "Add opensocial NewOrgWithoutCreator and Store.WithTx"
```

---

### Task 3: `spaces.Store.LockRepo` and `opensocial.Store.ProvisionMember`

**Files:**
- Modify: `internal/spaces/store.go` (the `Store` interface near line 92; the implementation next to `lockRepo` at line 441)
- Create: `internal/spaces/lock_repo_test.go`
- Create: `internal/opensocial/provision.go`
- Test: `internal/opensocial/provision_test.go`

**Interfaces:**
- Consumes: `putMembership` and `NewOrgWithoutCreator` (Task 2)
- Produces:
  - `spaces.Store.LockRepo(ctx context.Context, space habitat_syntax.SpaceURI, repo syntax.DID) error`
  - `func (s *opensocial.Store) ProvisionMember(ctx context.Context, orgDID, memberDID syntax.DID) error`

- [ ] **Step 1: Write the failing `LockRepo` test**

`internal/spaces/lock_repo_test.go`:

```go
package spaces_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// SQLite serializes whole transactions, so LockRepo is a no-op there; this
// pins that it's callable on a tx-scoped store without error.
func TestStoreLockRepo(t *testing.T) {
	db := db_testutil.NewDB(t)
	s := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db))
	owner := syntax.DID("did:plc:owner")
	space := habitat_syntax.ConstructSpaceURI(owner, "community.opensocial.members", "self")
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return s.WithTx(tx).LockRepo(t.Context(), space, owner)
	}))
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test ./internal/spaces/... -run TestStoreLockRepo`
Expected: FAIL (build error: `LockRepo` undefined)

- [ ] **Step 3: Export `LockRepo`**

In `internal/spaces/store.go`, add to the `Store` interface (next to `WithTx`):

```go
	// LockRepo acquires the per-(space, repo) advisory lock PutRecord and
	// DeleteRecord hold for their writes, held until the enclosing
	// transaction ends. It only serializes anything on a store scoped to a
	// transaction via WithTx, and is a no-op outside Postgres.
	LockRepo(ctx context.Context, space habitat_syntax.SpaceURI, repo syntax.DID) error
```

Add the implementation directly below `func lockRepo(...)`:

```go
// LockRepo implements [Store].
func (s *store) LockRepo(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) error {
	return lockRepo(s.db.WithContext(ctx), space, repo)
}
```

- [ ] **Step 4: Run the spaces tests and build everything**

Run: `go test ./internal/spaces/... && go build ./...`
Expected: PASS and a clean build. If another type claims to implement `spaces.Store`, the build names it; embed-based fakes are unaffected.

- [ ] **Step 5: Write the failing `ProvisionMember` tests**

`internal/opensocial/provision_test.go`:

```go
package opensocial_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func newOrgWithoutCreator(t *testing.T, s *opensocial_testutil.TestStore) syntax.DID {
	t.Helper()
	orgDIDStr, err := s.NewOrgWithoutCreator(t.Context(), "acme")
	require.NoError(t, err)
	return syntax.DID(orgDIDStr)
}

func TestStoreProvisionMember(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	org := newOrgWithoutCreator(t, s)
	membersSpace := habitat_syntax.ConstructSpaceURI(org, opensocial.MembersSpaceType, "self")
	first := syntax.DID("did:plc:first")
	second := syntax.DID("did:plc:second")

	// The first member of a creator-less org becomes its admin...
	require.NoError(t, s.ProvisionMember(t.Context(), org, first))
	roles, err := s.GetUserRoles(t.Context(), org, first)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)

	// ...with an acceptance record authored under their own repo, so the org
	// shows up in their member spaces.
	_, err = s.SpaceStore.GetRecord(
		t.Context(), membersSpace, first, opensocial.AcceptanceCollection, "self",
	)
	require.NoError(t, err)
	memberSpaces, err := s.ListMemberSpaces(t.Context(), first)
	require.NoError(t, err)
	require.Contains(t, memberSpaces, membersSpace)

	// Everyone after is a plain member.
	require.NoError(t, s.ProvisionMember(t.Context(), org, second))
	roles, err = s.GetUserRoles(t.Context(), org, second)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.MemberRoleRkey}, roles)
	roles, err = s.GetUserRoles(t.Context(), org, first)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)
}

func TestStoreProvisionMemberConcurrentFirstSignIns(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	org := newOrgWithoutCreator(t, s)

	const n = 5
	members := make([]syntax.DID, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		members[i] = syntax.DID(fmt.Sprintf("did:plc:member%d", i))
		wg.Go(func() { errs[i] = s.ProvisionMember(t.Context(), org, members[i]) })
	}
	wg.Wait()

	admins := 0
	for i, member := range members {
		require.NoError(t, errs[i])
		roles, err := s.GetUserRoles(t.Context(), org, member)
		require.NoError(t, err)
		if slices.Equal(roles, []string{opensocial.AdminRoleRkey}) {
			admins++
		} else {
			require.Equal(t, []string{opensocial.MemberRoleRkey}, roles)
		}
	}
	require.Equal(t, 1, admins)
}

// failAcceptanceStore is a spaces.Store that fails every write to the
// acceptance collection, to exercise ProvisionMember's rollback.
type failAcceptanceStore struct {
	spaces.Store
}

func (f failAcceptanceStore) WithTx(tx *gorm.DB) spaces.Store {
	return failAcceptanceStore{f.Store.WithTx(tx)}
}

func (f failAcceptanceStore) PutRecord(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	value spaces.MarshaledRecord,
) (habitat_syntax.SpaceRecordURI, *cid.Cid, error) {
	if collection == opensocial.AcceptanceCollection {
		return "", nil, errors.New("acceptance write failed")
	}
	return f.Store.PutRecord(ctx, space, owner, collection, rkey, value)
}

func TestStoreProvisionMemberRollsBack(t *testing.T) {
	base := opensocial_testutil.NewTestStore(t)
	org := newOrgWithoutCreator(t, base)
	failing := opensocial_testutil.NewTestStore(
		t,
		opensocial_testutil.WithDB(base.DB),
		opensocial_testutil.WithHive(base.Hive),
		opensocial_testutil.WithSpaceStore(failAcceptanceStore{base.SpaceStore}),
	)
	member := syntax.DID("did:plc:member")

	require.Error(t, failing.ProvisionMember(t.Context(), org, member))

	// The membership write was rolled back along with the failed acceptance.
	roles, err := base.GetUserRoles(t.Context(), org, member)
	require.NoError(t, err)
	require.Empty(t, roles)
}
```

- [ ] **Step 6: Run them and confirm they fail**

Run: `go test ./internal/opensocial/... -run TestStoreProvisionMember`
Expected: FAIL (build error: `s.ProvisionMember` undefined)

- [ ] **Step 7: Implement `ProvisionMember`**

`internal/opensocial/provision.go`:

```go
package opensocial

import (
	"context"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"gorm.io/gorm"
)

// ProvisionMember adds memberDID to orgDID by writing its membership record
// (authored by the org) and its own acceptance record (authored by the
// member) in a single transaction. Unlike RequestJoin, no prior invite is
// required — used when the caller has independent authority to enroll the
// member (e.g. the email-domain auto-provisioning flow, see
// identity.EmailResolver) and there's no invitee session to author the
// acceptance record under, so the backend authors both records directly.
//
// memberDID is granted the admin role if it is the org's first member
// (i.e. the org was created via NewOrgWithoutCreator and nobody has joined
// yet), and the member role otherwise. This check-and-write is made
// race-safe by acquiring the same per-(space, repo) advisory lock PutRecord
// itself takes before counting existing community.opensocial.membership
// records for orgDID, so two concurrent first sign-ins cannot both become
// admin.
func (s *Store) ProvisionMember(ctx context.Context, orgDID, memberDID syntax.DID) error {
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		spacesStoreTx := s.spacesStore.WithTx(tx)
		membersSpace := habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self")
		if err := spacesStoreTx.LockRepo(ctx, membersSpace, orgDID); err != nil {
			return fmt.Errorf("lock members repo: %w", err)
		}
		membershipNSID := syntax.NSID(MembershipCollection)
		existing, err := spacesStoreTx.ListRecords(ctx, membersSpace, orgDID, &membershipNSID)
		if err != nil {
			return fmt.Errorf("list memberships: %w", err)
		}
		roles := []string{MemberRoleRkey}
		if len(existing) == 0 {
			roles = []string{AdminRoleRkey}
		}
		if err := putMembership(ctx, spacesStoreTx, orgDID, memberDID, roles); err != nil {
			return err
		}
		recordBytes, err := spaces.MarshalRecord(opensocial_api.CommunityOpensocialAcceptance{
			UpdatedAt: time.Now().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal acceptance record: %w", err)
		}
		// repo only selects whose record namespace this lands in; it isn't a
		// credential check, so the backend can author it under memberDID.
		if _, _, err := spacesStoreTx.PutRecord(
			ctx, membersSpace, memberDID, AcceptanceCollection, "self", recordBytes,
		); err != nil {
			return fmt.Errorf("put acceptance record: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("provision member: %w", err)
	}
	return nil
}
```

- [ ] **Step 8: Run the opensocial tests and confirm they pass**

Run: `go test ./internal/opensocial/... -v -race`
Expected: PASS, including all three `TestStoreProvisionMember*` tests.

- [ ] **Step 9: Commit**

```bash
git add internal/spaces/store.go internal/spaces/lock_repo_test.go internal/opensocial/provision.go internal/opensocial/provision_test.go
git commit -m "Add opensocial ProvisionMember with first-member-is-admin"
```

---

### Task 4: `identity.EmailResolver` and the resolve endpoints' email fallback

**Files:**
- Create: `internal/identity/email.go`
- Modify: `internal/identity/server.go` (`Server` struct at :36, options at :46, `ResolveHandle` at :201, `ResolveIdentity` at :228)
- Test: `internal/identity/email_test.go`

**Interfaces:**
- Consumes: from Task 1, `emaildomain.Store` (`GetDID`, `LookupDomain`, `Transaction`, `WithTx`, `Provision`, `ErrEmailProvisioned`), `emaildomain.Email`, and `emaildomain.ParseEmail`. From Tasks 2–3, `opensocial.Store.WithTx` and `ProvisionMember`. From hive, `Hive.MintIdentity(ctx, handlePrefix, subdomain)` (prefix must match `^[a-zA-Z0-9]{1,50}$`; `hive.ErrNotCreated` on a taken handle), `Hive.LookupDID`, and `Hive.WithTx`.
- Produces:
  - `func NewEmailResolver(emailStore *emaildomain.Store, h hive.Hive, opensocialStore *opensocial.Store) *EmailResolver`
  - `func (r *EmailResolver) ResolveEmailIdentity(ctx context.Context, email emaildomain.Email) (*identity.Identity, error)`: returns indigo `identity.ErrDIDNotFound` when the domain isn't mapped
  - `func WithEmailResolver(r *EmailResolver) utils.Opt[Server]`

- [ ] **Step 1: Write the failing resolver and server tests**

`internal/identity/email_test.go`:

```go
package identity

import (
	"errors"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/hive"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type emailFixture struct {
	resolver   *EmailResolver
	emailStore *emaildomain.Store
	opensocial *opensocial_testutil.TestStore
	hive       hive.Hive
	org        syntax.DID
}

// newEmailFixture wires an EmailResolver over one shared DB, with a
// creator-less "acme" org mapped to acme.com.
func newEmailFixture(t *testing.T) emailFixture {
	t.Helper()
	db := db_testutil.NewDB(t)
	h, err := hive.NewHive("example.com", "pear.example.com", db)
	require.NoError(t, err)
	osStore := opensocial_testutil.NewTestStore(
		t, opensocial_testutil.WithDB(db), opensocial_testutil.WithHive(h),
	)
	emailStore, err := emaildomain.NewStore(db)
	require.NoError(t, err)
	orgDIDStr, err := osStore.NewOrgWithoutCreator(t.Context(), "acme")
	require.NoError(t, err)
	org := syntax.DID(orgDIDStr)
	require.NoError(t, emailStore.CreateDomainMapping(
		t.Context(), "acme.com", org, emaildomain.LoginMethodGoogle,
	))
	return emailFixture{
		resolver:   NewEmailResolver(emailStore, h, osStore.Store),
		emailStore: emailStore,
		opensocial: osStore,
		hive:       h,
		org:        org,
	}
}

func (f emailFixture) memberships(t *testing.T) int {
	t.Helper()
	membershipNSID := syntax.NSID(opensocial.MembershipCollection)
	records, err := f.opensocial.SpaceStore.ListRecords(
		t.Context(),
		habitat_syntax.ConstructSpaceURI(f.org, opensocial.MembersSpaceType, "self"),
		f.org,
		&membershipNSID,
	)
	require.NoError(t, err)
	return len(records)
}

func TestEmailResolverFirstSignInBecomesAdmin(t *testing.T) {
	f := newEmailFixture(t)
	ident, err := f.resolver.ResolveEmailIdentity(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.Equal(t, syntax.Handle("alice.acme.example.com"), ident.Handle)

	roles, err := f.opensocial.GetUserRoles(t.Context(), f.org, ident.DID)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)

	did, ok, err := f.emailStore.GetDID(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, ident.DID, did)

	served, err := f.hive.LookupDID(t.Context(), ident.DID)
	require.NoError(t, err)
	require.Equal(t, ident.DID, served.DID)
}

func TestEmailResolverLaterSignInsAreMembers(t *testing.T) {
	f := newEmailFixture(t)
	alice, err := f.resolver.ResolveEmailIdentity(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	bob, err := f.resolver.ResolveEmailIdentity(t.Context(), "bob@acme.com")
	require.NoError(t, err)
	require.NotEqual(t, alice.DID, bob.DID)

	roles, err := f.opensocial.GetUserRoles(t.Context(), f.org, bob.DID)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.MemberRoleRkey}, roles)
}

func TestEmailResolverReturningMember(t *testing.T) {
	f := newEmailFixture(t)
	first, err := f.resolver.ResolveEmailIdentity(t.Context(), "alice@acme.com")
	require.NoError(t, err)

	// Differently-cased input normalizes to the same email.
	email, err := emaildomain.ParseEmail("Alice@ACME.com")
	require.NoError(t, err)
	again, err := f.resolver.ResolveEmailIdentity(t.Context(), email)
	require.NoError(t, err)
	require.Equal(t, first.DID, again.DID)
	require.Equal(t, 1, f.memberships(t))
}

func TestEmailResolverUnknownDomain(t *testing.T) {
	f := newEmailFixture(t)
	_, err := f.resolver.ResolveEmailIdentity(t.Context(), "alice@other.com")
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
	_, ok, err := f.emailStore.GetDID(t.Context(), "alice@other.com")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestEmailResolverHandleCollision(t *testing.T) {
	f := newEmailFixture(t)
	alice, err := f.resolver.ResolveEmailIdentity(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	// "a.lice" sanitizes to the same "alice" handle prefix.
	other, err := f.resolver.ResolveEmailIdentity(t.Context(), "a.lice@acme.com")
	require.NoError(t, err)
	require.NotEqual(t, alice.DID, other.DID)
	require.Regexp(t, `^alice[0-9a-f]{8}\.acme\.example\.com$`, other.Handle.String())
}

func TestEmailResolverConcurrentSameEmail(t *testing.T) {
	f := newEmailFixture(t)
	var (
		wg      sync.WaitGroup
		results [2]*identity.Identity
		errs    [2]error
	)
	for i := range 2 {
		wg.Go(func() {
			results[i], errs[i] = f.resolver.ResolveEmailIdentity(t.Context(), "alice@acme.com")
		})
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, results[0].DID, results[1].DID)
	// The losing attempt rolled back its membership write.
	require.Equal(t, 1, f.memberships(t))
}

// noNetworkTransport fails every request, so overriddenDidDoc's spaces probe
// of a minted identity's PDS reads as "unsupported" without real network.
type noNetworkTransport struct{}

func (noNetworkTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("network disabled in tests")
}

func emailServer(f emailFixture, opts ...func(*Server)) *Server {
	s := &Server{
		hive:          f.hive,
		directory:     NewWrappedDirectory(f.hive, identity.NewMockDirectory()),
		domain:        "pear.domain",
		httpClient:    &http.Client{Transport: noNetworkTransport{}},
		emailResolver: f.resolver,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func TestResolveIdentityEmail(t *testing.T) {
	f := newEmailFixture(t)
	var out atproto.IdentityDefs_IdentityInfo
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		emailServer(f).ResolveIdentity,
		url.Values{"identifier": []string{"alice@acme.com"}},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "alice.acme.example.com", out.Handle)
	roles, err := f.opensocial.GetUserRoles(t.Context(), f.org, syntax.DID(out.Did))
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)
}

func TestResolveHandleEmail(t *testing.T) {
	f := newEmailFixture(t)
	var out atproto.IdentityResolveHandle_Output
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		emailServer(f).ResolveHandle,
		url.Values{"handle": []string{"alice@acme.com"}},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	did, ok, err := f.emailStore.GetDID(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, did.String(), out.Did)
}

func TestResolveIdentityEmailUnknownDomain(t *testing.T) {
	f := newEmailFixture(t)
	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		emailServer(f).ResolveIdentity,
		url.Values{"identifier": []string{"alice@other.com"}},
		&out,
	)
	require.Equal(t, http.StatusNotFound, code)
}

func TestResolveIdentityEmailDisabled(t *testing.T) {
	f := newEmailFixture(t)
	s := emailServer(f, func(s *Server) { s.emailResolver = nil })
	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ResolveIdentity,
		url.Values{"identifier": []string{"alice@acme.com"}},
		&out,
	)
	require.Equal(t, http.StatusBadRequest, code)
	_, ok, err := f.emailStore.GetDID(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.False(t, ok)
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./internal/identity/... -run 'Email'`
Expected: FAIL (build error: undefined `EmailResolver`, `NewEmailResolver`, and the `emailResolver` field)

- [ ] **Step 3: Implement `EmailResolver`**

`internal/identity/email.go`:

```go
package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/opensocial"
)

const (
	// handlePrefixMaxLen leaves room within hive's 50-char handle prefix
	// limit for an 8-char collision suffix.
	handlePrefixMaxLen = 42
	// mintAttempts bounds retries when a generated handle is already taken.
	mintAttempts = 5
)

// EmailResolver resolves a work email to the habitat identity provisioned
// for it, minting one and enrolling it in the email domain's org on first
// sight (see emaildomain.Store.CreateDomainMapping).
type EmailResolver struct {
	emailStore      *emaildomain.Store
	hive            hive.Hive
	opensocialStore *opensocial.Store
}

func NewEmailResolver(
	emailStore *emaildomain.Store,
	h hive.Hive,
	opensocialStore *opensocial.Store,
) *EmailResolver {
	return &EmailResolver{emailStore: emailStore, hive: h, opensocialStore: opensocialStore}
}

// ResolveEmailIdentity returns the identity provisioned for email, minting
// and enrolling one (admin if it's the org's first member, member
// otherwise) if email's domain is mapped to an org and email hasn't been
// seen before. It returns identity.ErrDIDNotFound if the domain isn't
// mapped.
func (r *EmailResolver) ResolveEmailIdentity(
	ctx context.Context,
	email emaildomain.Email,
) (*identity.Identity, error) {
	if ident, ok, err := r.lookupProvisioned(ctx, email); err != nil || ok {
		return ident, err
	}
	orgDID, _, ok, err := r.emailStore.LookupDomain(ctx, email.Domain())
	if err != nil {
		return nil, fmt.Errorf("lookup email domain: %w", err)
	}
	if !ok {
		return nil, identity.ErrDIDNotFound
	}
	orgIdent, err := r.hive.LookupDID(ctx, orgDID)
	if err != nil {
		return nil, fmt.Errorf("lookup org identity: %w", err)
	}
	// Org handles are a single label minted as "<label>.<memberDomain>";
	// members are minted beneath it, e.g. "alice.acme.<memberDomain>".
	orgLabel, _, _ := strings.Cut(orgIdent.Handle.String(), ".")

	var minted *identity.Identity
	err = r.emailStore.Transaction(ctx, func(tx *gorm.DB) error {
		ident, err := mintMemberIdentity(ctx, r.hive.WithTx(tx), email, orgLabel)
		if err != nil {
			return err
		}
		if err := r.opensocialStore.WithTx(tx).ProvisionMember(ctx, orgDID, ident.DID); err != nil {
			return err
		}
		if err := r.emailStore.WithTx(tx).Provision(ctx, email, orgDID, ident.DID); err != nil {
			return err
		}
		minted = ident
		return nil
	})
	if errors.Is(err, emaildomain.ErrEmailProvisioned) {
		// A concurrent first sign-in for the same email won; ours rolled
		// back, so return theirs.
		ident, ok, err := r.lookupProvisioned(ctx, email)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("email %s provisioned concurrently but not found", email)
		}
		return ident, nil
	}
	if err != nil {
		return nil, fmt.Errorf("provision %s: %w", email, err)
	}
	return minted, nil
}

// lookupProvisioned returns the identity email was already provisioned as,
// if any.
func (r *EmailResolver) lookupProvisioned(
	ctx context.Context,
	email emaildomain.Email,
) (*identity.Identity, bool, error) {
	did, ok, err := r.emailStore.GetDID(ctx, email)
	if err != nil {
		return nil, false, fmt.Errorf("get provisioned did: %w", err)
	}
	if !ok {
		return nil, false, nil
	}
	ident, err := r.hive.LookupDID(ctx, did)
	if err != nil {
		return nil, false, fmt.Errorf("lookup provisioned identity: %w", err)
	}
	return ident, true, nil
}

// mintMemberIdentity mints an identity under orgLabel whose handle is
// derived from email's local part, adding a random suffix if that's taken.
func mintMemberIdentity(
	ctx context.Context,
	h hive.Hive,
	email emaildomain.Email,
	orgLabel string,
) (*identity.Identity, error) {
	base := handlePrefix(email.LocalPart())
	candidate := base
	for range mintAttempts {
		ident, err := h.MintIdentity(ctx, candidate, orgLabel)
		if err == nil {
			return ident, nil
		}
		if !errors.Is(err, hive.ErrNotCreated) {
			return nil, fmt.Errorf("mint member identity: %w", err)
		}
		suffix := make([]byte, 4)
		if _, err := rand.Read(suffix); err != nil {
			return nil, fmt.Errorf("generate handle suffix: %w", err)
		}
		candidate = base + hex.EncodeToString(suffix)
	}
	return nil, fmt.Errorf("mint member identity for %s: handle taken after %d attempts", email, mintAttempts)
}

// handlePrefix keeps only the characters hive allows in a handle prefix
// ([a-z0-9], since email is already lowercased), falling back to "member"
// when none remain.
func handlePrefix(localPart string) string {
	var b strings.Builder
	for _, c := range localPart {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		}
		if b.Len() == handlePrefixMaxLen {
			break
		}
	}
	if b.Len() == 0 {
		return "member"
	}
	return b.String()
}
```

- [ ] **Step 4: Wire the fallback into `Server`**

In `internal/identity/server.go`:

Add the import `"github.com/habitat-network/habitat/internal/emaildomain"`, and add a field to `Server` after `httpClient`:

```go
	// emailResolver, if set, lets identifiers that aren't handles/DIDs
	// resolve as work emails (see EmailResolver).
	emailResolver *EmailResolver
```

After `WithClient`, add:

```go
// WithEmailResolver lets resolveIdentity/resolveHandle accept a work email in
// place of a handle, resolving it (and minting on first sight) via r.
func WithEmailResolver(r *EmailResolver) utils.Opt[Server] {
	return func(s *Server) {
		s.emailResolver = r
	}
}

// parseEmail reports whether identifier should resolve as a work email:
// email resolution is configured and identifier parses as one.
func (s *Server) parseEmail(identifier string) (emaildomain.Email, bool) {
	if s.emailResolver == nil {
		return "", false
	}
	email, err := emaildomain.ParseEmail(identifier)
	return email, err == nil
}
```

In `ResolveHandle`, replace

```go
	handle, err := syntax.ParseHandle(handleStr)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid handle", err)
		return
	}
	ident, err := s.directory.LookupHandle(ctx, handle)
```

with

```go
	var ident *identity.Identity
	handle, err := syntax.ParseHandle(handleStr)
	if err == nil {
		ident, err = s.directory.LookupHandle(ctx, handle)
	} else if email, ok := s.parseEmail(handleStr); ok {
		ident, err = s.emailResolver.ResolveEmailIdentity(ctx, email)
		if errors.Is(err, identity.ErrDIDNotFound) {
			// an email whose domain isn't mapped reads as an unknown handle
			err = identity.ErrHandleNotFound
		}
	} else {
		httpx.WriteInvalidRequest(ctx, w, "invalid handle", err)
		return
	}
```

In `ResolveIdentity`, replace

```go
	atid, err := syntax.ParseAtIdentifier(identifier)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid identifier", err)
		return
	}
	ident, err := s.directory.Lookup(ctx, atid)
```

with

```go
	var ident *identity.Identity
	atid, err := syntax.ParseAtIdentifier(identifier)
	if err == nil {
		ident, err = s.directory.Lookup(ctx, atid)
	} else if email, ok := s.parseEmail(identifier); ok {
		ident, err = s.emailResolver.ResolveEmailIdentity(ctx, email)
	} else {
		httpx.WriteInvalidRequest(ctx, w, "invalid identifier", err)
		return
	}
```

The rest of both handlers (error mapping and response) stays unchanged.

- [ ] **Step 5: Run the identity tests and confirm they pass**

Run: `go test ./internal/identity/... -v -race`
Expected: PASS: all new `*Email*` tests plus the existing `TestResolveDID`, `TestResolveHandle`, `TestResolveIdentity`, and `TestResolveHandleNotFound`.

- [ ] **Step 6: Commit**

```bash
git add internal/identity/email.go internal/identity/email_test.go internal/identity/server.go
git commit -m "Resolve work emails to provisioned identities in identity server"
```

---

### Task 5: `org.LoginRouter` email branch

**Files:**
- Modify: `internal/org/login_router.go`
- Test: `internal/org/login_router_test.go` (append)

**Interfaces:**
- Consumes: `emaildomain.Store.GetLoginMethod`, `GetEmail`, `emaildomain.LoginMethodGoogle` (Task 1); `login_testutil.PassthroughProvider`, whose `Authorize` sets `LoginID = loginHint` when `LoginID` is empty and whose `Exchange` returns `LoginID`
- Produces: a new `LoginRouter` field `EmailStore *emaildomain.Store`. `nil` disables the email branch, which keeps existing callers and tests working.

- [ ] **Step 1: Write the failing test**

Append to `internal/org/login_router_test.go` (add the imports `"github.com/bluesky-social/indigo/atproto/syntax"`, `db_testutil "github.com/habitat-network/habitat/internal/db/testutil"`, and `"github.com/habitat-network/habitat/internal/emaildomain"`):

```go
func TestLoginRouterEmailDomain(t *testing.T) {
	emailStore, err := emaildomain.NewStore(db_testutil.NewDB(t))
	require.NoError(t, err)
	orgDID := syntax.DID("did:web:acme.example.com")
	alice := syntax.DID("did:web:alice.example.com")
	require.NoError(t, emailStore.CreateDomainMapping(
		t.Context(), "acme.com", orgDID, emaildomain.LoginMethodGoogle,
	))
	require.NoError(t, emailStore.Provision(t.Context(), "alice@acme.com", orgDID, alice))
	orgStore := testutil.NewTestStore(t)

	t.Run("google login for provisioned email", func(t *testing.T) {
		p := login_testutil.NewPassthroughProvider(t)
		router := org.LoginRouter{Google: p, OrgStore: orgStore, EmailStore: emailStore}
		_, state, err := router.Authorize(t.Context(), alice)
		require.NoError(t, err)
		// The provisioned email is passed as Google's login_hint.
		require.Equal(t, "alice@acme.com", p.LoginID)
		require.NoError(t, router.Exchange(t.Context(), alice, url.Values{}, state))
	})

	t.Run("google email differing only in case is accepted", func(t *testing.T) {
		p := login_testutil.NewPassthroughProvider(t)
		p.LoginID = "Alice@Acme.com"
		router := org.LoginRouter{Google: p, OrgStore: orgStore, EmailStore: emailStore}
		require.NoError(t, router.Exchange(t.Context(), alice, url.Values{}, nil))
	})

	t.Run("mismatched google email is rejected", func(t *testing.T) {
		p := login_testutil.NewPassthroughProvider(t)
		p.LoginID = "mallory@acme.com"
		router := org.LoginRouter{Google: p, OrgStore: orgStore, EmailStore: emailStore}
		require.Error(t, router.Exchange(t.Context(), alice, url.Values{}, nil))
	})

	t.Run("google not configured", func(t *testing.T) {
		router := org.LoginRouter{OrgStore: orgStore, EmailStore: emailStore}
		_, _, err := router.Authorize(t.Context(), alice)
		require.ErrorContains(t, err, "unsupported login provider")
		err = router.Exchange(t.Context(), alice, url.Values{}, nil)
		require.ErrorContains(t, err, "unsupported login provider")
	})
}
```

The existing `TestLoginRouter` subtests are unchanged: they build routers without `EmailStore`, which pins that the org/member paths still work when it's nil.

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test ./internal/org/... -run TestLoginRouterEmailDomain`
Expected: FAIL (build error: unknown field `EmailStore`)

- [ ] **Step 3: Implement the email branch**

In `internal/org/login_router.go`, add the imports `"strings"` and `"github.com/habitat-network/habitat/internal/emaildomain"`, then:

Change the struct to:

```go
type LoginRouter struct {
	Pds      login.Provider
	Google   login.Provider
	Password login.Provider
	OrgStore Store
	// EmailStore, if set, routes DIDs provisioned via email-domain sign-in
	// (see identity.EmailResolver) through their domain's login method.
	EmailStore *emaildomain.Store
}
```

Add after `getProvider`:

```go
// emailProvider returns the login provider for an email domain's login
// method, or nil if it isn't configured on this instance.
func (r *LoginRouter) emailProvider(method emaildomain.LoginMethod) login.Provider {
	switch method {
	case emaildomain.LoginMethodGoogle:
		return r.Google
	}
	return nil
}

// emailLogin returns the login provider and provisioned email for did if it
// was provisioned via email-domain sign-in; ok is false for any other DID.
func (r *LoginRouter) emailLogin(
	ctx context.Context,
	did syntax.DID,
) (login.Provider, emaildomain.Email, bool, error) {
	if r.EmailStore == nil {
		return nil, "", false, nil
	}
	method, ok, err := r.EmailStore.GetLoginMethod(ctx, did)
	if err != nil {
		return nil, "", false, fmt.Errorf("get email login method: %w", err)
	}
	if !ok {
		return nil, "", false, nil
	}
	email, ok, err := r.EmailStore.GetEmail(ctx, did)
	if err != nil {
		return nil, "", false, fmt.Errorf("get provisioned email: %w", err)
	}
	if !ok {
		return nil, "", false, fmt.Errorf("no email provisioned for %s", did)
	}
	provider := r.emailProvider(method)
	if provider == nil {
		return nil, "", false, fmt.Errorf("unsupported login provider for %s", did)
	}
	return provider, email, true, nil
}
```

At the top of `Authorize`, before `// org login (requires admin credential)`, insert:

```go
	// email-domain member login
	if provider, email, ok, err := r.emailLogin(ctx, did); err != nil {
		return "", nil, err
	} else if ok {
		return provider.Authorize(ctx, string(email))
	}

```

At the top of `Exchange`, before `// org login (requires admin)`, insert:

```go
	// email-domain member login
	if provider, email, ok, err := r.emailLogin(ctx, did); err != nil {
		return err
	} else if ok {
		loginID, err := provider.Exchange(ctx, query, state)
		if err != nil {
			return fmt.Errorf("failed to exchange code: %w", err)
		}
		if !strings.EqualFold(loginID, string(email)) {
			return fmt.Errorf("login id mismatch: %s != %s", email, loginID)
		}
		return nil
	}

```

- [ ] **Step 4: Run the org tests and confirm they pass**

Run: `go test ./internal/org/... -v -run TestLoginRouter`
Expected: PASS (`TestLoginRouter/*` and `TestLoginRouterEmailDomain/*`)

- [ ] **Step 5: Commit**

```bash
git add internal/org/login_router.go internal/org/login_router_test.go
git commit -m "Route email-provisioned DIDs through Google in LoginRouter"
```

---

### Task 6: OAuth login hint accepts a work email

**Files:**
- Modify: `internal/oauthserver/oauth_server.go` (`OAuthServer` struct, `NewOAuthServer`, `resolveLoginHint` at :355 and its 3 callers)
- Modify: `internal/oauthserver/oauth_server_test.go` (every `NewOAuthServer(` call)
- Create: `internal/oauthserver/login_hint_test.go`

**Interfaces:**
- Consumes: `emaildomain.Email`, `emaildomain.ParseEmail` (Task 1). `*identity.EmailResolver` from Task 4 satisfies the new interface structurally (Task 8 passes it in).
- Produces:
  - `type EmailIdentityResolver interface { ResolveEmailIdentity(ctx context.Context, email emaildomain.Email) (*identity.Identity, error) }` (indigo `identity`)
  - `NewOAuthServer(..., opensocialStore *opensocial.Store, emailResolver EmailIdentityResolver)`: new **last** parameter; `nil` disables email login hints
  - `func (o *OAuthServer) resolveLoginHint(ctx context.Context, loginHint string) (syntax.DID, error)`

- [ ] **Step 1: Write the failing test**

`internal/oauthserver/login_hint_test.go`:

```go
package oauthserver

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/emaildomain"
)

// fakeEmailResolver resolves a fixed set of emails.
type fakeEmailResolver map[emaildomain.Email]syntax.DID

func (f fakeEmailResolver) ResolveEmailIdentity(
	ctx context.Context,
	email emaildomain.Email,
) (*identity.Identity, error) {
	did, ok := f[email]
	if !ok {
		return nil, identity.ErrDIDNotFound
	}
	return &identity.Identity{DID: did}, nil
}

func TestResolveLoginHintEmail(t *testing.T) {
	alice := syntax.DID("did:web:alice.example.com")
	o := &OAuthServer{
		directory:     identity.NewMockDirectory(),
		emailResolver: fakeEmailResolver{"alice@acme.com": alice},
	}

	did, err := o.resolveLoginHint(t.Context(), "Alice@acme.com")
	require.NoError(t, err)
	require.Equal(t, alice, did)

	_, err = o.resolveLoginHint(t.Context(), "bob@other.com")
	require.ErrorIs(t, err, identity.ErrDIDNotFound)

	// Without a resolver, emails keep resolving to no subject, as before.
	o.emailResolver = nil
	did, err = o.resolveLoginHint(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.Empty(t, did)
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test ./internal/oauthserver/... -run TestResolveLoginHintEmail`
Expected: FAIL (build error: unknown field `emailResolver`; `resolveLoginHint` called with too many arguments)

- [ ] **Step 3: Implement**

In `internal/oauthserver/oauth_server.go`:

Add the import `"github.com/habitat-network/habitat/internal/emaildomain"`.

Above the `OAuthServer` struct, add:

```go
// EmailIdentityResolver resolves a work email typed as a login hint to the
// identity provisioned for it (see identity.EmailResolver).
type EmailIdentityResolver interface {
	ResolveEmailIdentity(ctx context.Context, email emaildomain.Email) (*identity.Identity, error)
}
```

Add a field to `OAuthServer` after `opensocialStore`:

```go
	// emailResolver, if set, lets login hints be work emails.
	emailResolver EmailIdentityResolver
```

Add `emailResolver EmailIdentityResolver,` as the new last parameter of `NewOAuthServer` (after `opensocialStore *opensocial.Store,`). Set `emailResolver: emailResolver,` in the returned `&OAuthServer{...}` literal next to `opensocialStore`. Also add this line to the doc comment's parameter list:

```go
//   - emailResolver: resolves work-email login hints; nil disables them
```

Thread `ctx` into the three callers with a sed script (the definition line doesn't match because it reads `) resolveLoginHint(`):

```bash
sed -i '' 's/o\.resolveLoginHint(/o.resolveLoginHint(ctx, /g' internal/oauthserver/oauth_server.go
```

Replace `resolveLoginHint` with:

```go
func (o *OAuthServer) resolveLoginHint(ctx context.Context, loginHint string) (syntax.DID, error) {
	if loginHint == "" {
		return "", nil
	}
	if atid, err := syntax.ParseAtIdentifier(loginHint); err == nil {
		id, err := o.directory.Lookup(ctx, atid)
		if err != nil {
			return "", fmt.Errorf("failed to lookup handle: %w", err)
		}
		return id.DID, nil
	}
	// A work email resolves (minting on first sight) to the identity
	// provisioned for it in its domain's org; see identity.EmailResolver.
	if email, err := emaildomain.ParseEmail(loginHint); err == nil && o.emailResolver != nil {
		id, err := o.emailResolver.ResolveEmailIdentity(ctx, email)
		if err != nil {
			return "", fmt.Errorf("failed to resolve email: %w", err)
		}
		return id.DID, nil
	}
	return "", nil
}
```

Append `nil` to every `NewOAuthServer(` call in the tests. Each call currently ends in `\t\ttestOpensocialStore(t),\n\t)`:

```bash
perl -0pi -e 's/(\t\ttestOpensocialStore\(t\),\n)(\t\))/$1\t\tnil,\n$2/g' internal/oauthserver/oauth_server_test.go
```

- [ ] **Step 4: Build and run the oauthserver tests**

Run: `go vet ./internal/oauthserver/... && go test ./internal/oauthserver/...`
Expected: PASS. If `go vet` reports "not enough arguments in call to NewOAuthServer", that call has a different trailing shape: add `nil,` as its last argument by hand. `cmd/pear/main.go` won't compile until Task 8; that's expected.

- [ ] **Step 5: Commit**

```bash
git add internal/oauthserver
git commit -m "Accept work-email login hints in OAuth server"
```

---

### Task 7: `network.habitat.emaildomain.createOrg` endpoint

**Files:**
- Create: `lexicons/network/habitat/emaildomain/createOrg.json`
- Generated (by `moon :generate`; commit them, don't edit): `api/habitat/emaildomain_createOrg.go`, `typescript/api/src/generated/...`, `typescript/xrpc-openapi-gen/spec/api.json`, `api-docs/static/api.json`
- Create: `internal/pearserver/emaildomain_create_org.go`
- Modify: `internal/pearserver/server.go` (struct + `New`), `internal/pearserver/testutil/server.go`
- Test: `internal/pearserver/emaildomain_create_org_test.go`

**Interfaces:**
- Consumes: `opensocial.Store.NewOrgWithoutCreator` (Task 2); `emaildomain.Store.LookupDomain`, `CreateDomainMapping`, `ErrDomainTaken`, `LoginMethodGoogle` (Task 1); `parseHandle(ctx, w, handle) bool` (`internal/pearserver/opensocial_create_org.go:48`); `httpx.WriteError(ctx, w, name, msg, code)`
- Produces:
  - `habitat.NetworkHabitatEmaildomainCreateOrgInput{Handle, Domain string}` / `...Output{Org string}` (generated)
  - `func (p *PearServer) CreateEmailDomainOrg(w http.ResponseWriter, r *http.Request)`
  - `pearserver.New(..., pdsForwarding *forwarding.PDSForwarding, emailDomainStore *emaildomain.Store)`: new **last** parameter
  - `pearserver_testutil.TestServer.EmailDomainStore *emaildomain.Store`

- [ ] **Step 1: Add the lexicon and generate**

`lexicons/network/habitat/emaildomain/createOrg.json`:

```json
{
    "lexicon": 1,
    "id": "network.habitat.emaildomain.createOrg",
    "defs": {
        "main": {
            "type": "procedure",
            "description": "Create a new opensocial-backed org mapped to an email domain. The org starts with no members: the first user to sign in with a Google-verified email at the domain becomes its admin, and later ones become members. Requires OAuth or service-auth.",
            "input": {
                "encoding": "application/json",
                "schema": {
                    "type": "object",
                    "required": ["handle", "domain"],
                    "properties": {
                        "handle": {
                            "type": "string",
                            "description": "Subdomain handle for the org (alphanumeric, 1-50 chars)."
                        },
                        "domain": {
                            "type": "string",
                            "description": "Email domain whose users sign in to this org, e.g. \"acme.com\"."
                        }
                    }
                }
            },
            "output": {
                "encoding": "application/json",
                "schema": {
                    "type": "object",
                    "required": ["org"],
                    "properties": {
                        "org": {
                            "type": "string",
                            "format": "did",
                            "description": "DID of the created org."
                        }
                    }
                }
            },
            "errors": [
                { "name": "DomainTaken", "description": "The email domain is already mapped to an org." }
            ]
        }
    }
}
```

Run: `moon :generate && grep -n "type NetworkHabitatEmaildomainCreateOrg" api/habitat/*.go`
Expected: `NetworkHabitatEmaildomainCreateOrgInput` and `NetworkHabitatEmaildomainCreateOrgOutput` are both listed. If the generated names differ, use the generated names everywhere below.

- [ ] **Step 2: Add the `emailDomainStore` dependency**

In `internal/pearserver/server.go`, add the import `"github.com/habitat-network/habitat/internal/emaildomain"`, and add a field after `pdsForwarding`:

```go
	// emailDomainStore maps email domains to orgs for email-based sign-in.
	emailDomainStore *emaildomain.Store
```

Add `emailDomainStore *emaildomain.Store,` as the new last parameter of `New`, and set `emailDomainStore: emailDomainStore,` in the `&PearServer{...}` literal.

In `internal/pearserver/testutil/server.go`:
- add the import `"github.com/habitat-network/habitat/internal/emaildomain"`
- add the field `EmailDomainStore *emaildomain.Store` to `TestServer` (after `PDSForwarding`)
- just before `ts.Server = pearserver.New(`, insert:

```go
	emailDomainStore, err := emaildomain.NewStore(ts.DB)
	require.NoError(t, err)
	ts.EmailDomainStore = emailDomainStore
```

- pass `emailDomainStore,` as the last argument to `pearserver.New(` (after `ts.PDSForwarding,`)

- [ ] **Step 3: Write the failing handler test**

`internal/pearserver/emaildomain_create_org_test.go`:

```go
package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	"github.com/habitat-network/habitat/internal/emaildomain"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
)

func TestServer_CreateEmailDomainOrg(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	create := func(
		ts *pearserver_testutil.TestServer,
		handle, domain string,
	) (habitat.NetworkHabitatEmaildomainCreateOrgOutput, int) {
		var out habitat.NetworkHabitatEmaildomainCreateOrgOutput
		code := client.Procedure(
			ts.Server.CreateEmailDomainOrg,
			habitat.NetworkHabitatEmaildomainCreateOrgInput{Handle: handle, Domain: domain},
			&out,
		)
		return out, code
	}

	t.Run("creates org mapped to domain with no members", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		out, code := create(ts, "acme", "ACME.com")
		require.Equal(t, http.StatusOK, code)
		org := syntax.DID(out.Org)

		isOrg, err := ts.OpenSocialStore.IsOrg(t.Context(), org)
		require.NoError(t, err)
		require.True(t, isOrg)

		// The caller is not made a member; the first email sign-in will be admin.
		roles, err := ts.OpenSocialStore.GetUserRoles(t.Context(), org, alice)
		require.NoError(t, err)
		require.Empty(t, roles)

		mapped, method, ok, err := ts.EmailDomainStore.LookupDomain(t.Context(), "acme.com")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, org, mapped)
		require.Equal(t, emaildomain.LoginMethodGoogle, method)
	})

	t.Run("rejects an already-mapped domain", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		_, code := create(ts, "acme", "acme.com")
		require.Equal(t, http.StatusOK, code)
		_, code = create(ts, "acme2", "acme.com")
		require.Equal(t, http.StatusConflict, code)
	})

	t.Run("requires auth", func(t *testing.T) {
		ts := pearserver_testutil.NewTestServer(
			t,
			pearserver_testutil.WithValidator(authntest.NewFailureValidator()),
		)
		_, code := create(ts, "acme", "acme.com")
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("rejects invalid input", func(t *testing.T) {
		tests := []struct {
			name, handle, domain string
		}{
			{"empty handle", "", "acme.com"},
			{"dotted handle", "sub.acme", "acme.com"},
			{"empty domain", "acme", ""},
			{"single-label domain", "acme", "localhost"},
			{"invalid domain", "acme", "not a domain!"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ts := newOpenSocialServer(t, alice)
				_, code := create(ts, tt.handle, tt.domain)
				require.Equal(t, http.StatusBadRequest, code)
			})
		}
	})
}
```

- [ ] **Step 4: Run it and confirm it fails**

Run: `go test ./internal/pearserver/... -run TestServer_CreateEmailDomainOrg`
Expected: FAIL (build error: `ts.Server.CreateEmailDomainOrg` undefined)

- [ ] **Step 5: Implement the handler**

`internal/pearserver/emaildomain_create_org.go`:

```go
package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/httpx"
)

// CreateEmailDomainOrg implements network.habitat.emaildomain.createOrg. Any
// authenticated user may call it: mapping a domain grants the caller nothing,
// since only someone completing Google OAuth for an address at the domain
// can ever sign in to the org, and the first such sign-in becomes admin.
func (p *PearServer) CreateEmailDomainOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r); !ok {
		return
	}
	var input habitat.NetworkHabitatEmaildomainCreateOrgInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	if !parseHandle(ctx, w, input.Handle) {
		return
	}
	// Handle syntax is DNS-name syntax, which is what an email domain must be.
	domain, err := syntax.ParseHandle(strings.ToLower(input.Domain))
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, fmt.Sprintf("invalid domain: %s", input.Domain), err)
		return
	}
	// Checked up front so a taken domain doesn't leave behind an orphan org;
	// CreateDomainMapping still catches a concurrent claim.
	_, _, taken, err := p.emailDomainStore.LookupDomain(ctx, domain.String())
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("lookup domain: %w", err))
		return
	}
	if taken {
		writeDomainTaken(ctx, w)
		return
	}
	org, err := p.opensocialStore.NewOrgWithoutCreator(ctx, input.Handle)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("new org: %w", err))
		return
	}
	err = p.emailDomainStore.CreateDomainMapping(
		ctx, domain.String(), syntax.DID(org), emaildomain.LoginMethodGoogle,
	)
	if errors.Is(err, emaildomain.ErrDomainTaken) {
		writeDomainTaken(ctx, w)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("create domain mapping: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatEmaildomainCreateOrgOutput{Org: org})
}

func writeDomainTaken(ctx context.Context, w http.ResponseWriter) {
	httpx.WriteError(
		ctx, w, "DomainTaken", "email domain is already mapped to an org", http.StatusConflict,
	)
}
```

(Add `"context"` to the imports for `writeDomainTaken`.)

- [ ] **Step 6: Run the pearserver tests and confirm they pass**

Run: `go test ./internal/pearserver/...`
Expected: PASS (new test plus all existing pearserver tests, which now build through the updated `testutil`)

- [ ] **Step 7: Commit**

```bash
git add lexicons/network/habitat/emaildomain api/habitat typescript/api typescript/xrpc-openapi-gen/spec api-docs/static internal/pearserver
git commit -m "Add network.habitat.emaildomain.createOrg endpoint"
```

---

### Task 8: Wire it up in `cmd/pear` and verify end to end

**Files:**
- Modify: `cmd/pear/main.go` (around :266 `loginRouter`, :318 `opensocialStore`, :325 `NewOAuthServer`, :380 `pearserver.New`, :426 routes, :446 `habitat_identity.NewServer`)
- Modify: `CLAUDE.md` (package list)

**Interfaces:**
- Consumes: everything above. In particular `emaildomain.NewStore`, `habitat_identity.NewEmailResolver`, `habitat_identity.WithEmailResolver`, the new last parameters of `NewOAuthServer` and `pearserver.New`, `org.LoginRouter.EmailStore`, and `PearServer.CreateEmailDomainOrg`
- Produces: a running pear that serves email-domain sign-in

- [ ] **Step 1: Wire the store, resolver, and route**

In `cmd/pear/main.go`, add the import `"github.com/habitat-network/habitat/internal/emaildomain"`.

Immediately before `loginRouter := &org.LoginRouter{`, add:

```go
	emailDomainStore, err := emaildomain.NewStore(db.WithContext(startupCtx))
	if err != nil {
		return fmt.Errorf("setup email domain store: %w", err)
	}
```

and add `EmailStore: emailDomainStore,` to the `org.LoginRouter{...}` literal.

Immediately after the `opensocialStore, err := opensocial.NewStore(...)` error check, add:

```go
	emailResolver := habitat_identity.NewEmailResolver(emailDomainStore, hive, opensocialStore)
```

Pass `emailResolver,` as the last argument to `oauthserver.NewOAuthServer(` (after `opensocialStore,`), and `emailDomainStore,` as the last argument to `pearserver.New(` (after `pdsForwarding,`).

Next to the `network.habitat.opensocial.createOrg` route, add:

```go
	mux.HandleFunc("/xrpc/network.habitat.emaildomain.createOrg", pearApp.CreateEmailDomainOrg)
```

Change the identity server construction to:

```go
	idServer, err := habitat_identity.NewServer(
		hive, validator, orgStore, pdsForwarding, domain,
		habitat_identity.WithClient(httpx.NewClient()),
		habitat_identity.WithEmailResolver(emailResolver),
	)
```

- [ ] **Step 2: Document the package**

In `CLAUDE.md`, under **Go packages → Identity & org**, after the `org` bullet add:

```markdown
- `emaildomain` — email-domain → org mappings and email → DID provisioning for Google work-email sign-in (first sign-in becomes org admin); see `identity.EmailResolver`
```

- [ ] **Step 3: Build, test, lint**

Run: `go build ./... && go test ./... && golangci-lint run`
Expected: build OK, all tests PASS, no lint findings. `bodyclose` and `sloglint` are enabled, and the new code adds no HTTP bodies or slog calls, so any finding is a real issue to fix.

- [ ] **Step 4: Check coverage thresholds**

Run: `go test -coverprofile=cover.out ./internal/emaildomain/... ./internal/identity/... ./internal/opensocial/... ./internal/pearserver/... && go tool cover -func=cover.out | grep -E 'emaildomain|identity/email|opensocial/provision|emaildomain_create_org'`
Expected: each new file ≥ 60%, packages ≥ 70% (`.testcoverage.yml`). Then `rm cover.out` (don't commit it).

- [ ] **Step 5: Commit**

```bash
git add cmd/pear/main.go CLAUDE.md
git commit -m "Wire email-domain sign-in into pear"
```

- [ ] **Step 6: Manual smoke test (dev)**

With `HABITAT_GOOGLE_CLIENT_ID`/`HABITAT_GOOGLE_CLIENT_SECRET` set in `dev.env`, run `moon pear:dev`. As any logged-in user, call `network.habitat.emaildomain.createOrg` with `{"handle":"acmetest","domain":"<your google workspace domain>"}`. Then start an OAuth login from the frontend with your work email as the login hint. You should be redirected to Google with your email preselected, land back signed in, and see yourself as admin of the org. Report the outcome, including any step that couldn't be run.
