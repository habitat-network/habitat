# OverrideDirectory Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract the DID-override and email-resolution logic out of `internal/identity/server.go` into a new `OverrideDirectory` implementing indigo's `identity.Directory`, reducing the server's resolve handlers to thin lookups plus HTTP error mapping.

**Architecture:** A new `OverrideDirectory` wraps the existing `WrappedDirectory` (hive → public atproto directory). Every lookup delegates to the wrapped directory and then rewrites the returned identity's `Services` so `#atproto_pds` points at this habitat instance — unless the real PDS supports spaces. Email resolution lives on non-interface methods (`LookupEmail`, `LookupIdentifier`) because indigo's `Directory` interface only accepts handles/DIDs/at-identifiers, none of which can represent an email. The server keeps only param parsing, error-to-HTTP mapping, and serialization of `ident.DIDDocument()`.

**Tech Stack:** Go 1.26/1.27, indigo `atproto/identity` (v0.0.0-20260818202247), testify, moon/golangci-lint (proto-managed).

**Design doc:** `docs/superpowers/specs/2026-09-24-override-directory-design.md`

---

## File Structure

| File | Responsibility | Change |
| --- | --- | --- |
| `internal/identity/override_dir.go` | New `OverrideDirectory` type: constructor, `identity.Directory` interface methods, `LookupEmail`/`LookupIdentifier`, `applyOverride` | **Create** |
| `internal/identity/override_dir_test.go` | Unit tests for `OverrideDirectory` | **Create** |
| `internal/identity/server.go` | `Server` rewritten to serve lookups through `OverrideDirectory`; removes `parseEmail`, `overriddenDidDoc`, and the `emailResolver`/`domain`/`httpClient` fields | **Modify** |
| `internal/identity/server_test.go` | `testResolveServer` fixture wraps its mock directory in `NewOverrideDirectory` | **Modify** |
| `internal/identity/email_test.go` | `emailServer` fixture wraps its directory in `NewOverrideDirectory`; `TestResolveIdentityEmailDisabled` disables email via the directory | **Modify** |

`cmd/pear/main.go` and `integration/org_mint_identity_test.go` compile unchanged.

---

## Task 1: Add `OverrideDirectory` with DID override and email/identifier lookups

Add the new type and its tests. The server is untouched in this task. To avoid a name collision (the server still owns `WithClient`/`WithEmailResolver` in this task), `NewOverrideDirectory` takes no option funcs yet; tests configure the unexported `httpClient`/`emailResolver` fields directly (same package).

**Files:**
- Create: `internal/identity/override_dir.go`
- Create: `internal/identity/override_dir_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/identity/override_dir_test.go`:

```go
package identity

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

// unsupportedPDS is an httptest server that serves 404s, so SupportsSpaces
// reads it as "does not implement the spaces protocol".
func unsupportedPDS(t *testing.T) *httptest.Server {
	t.Helper()
	pds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(pds.Close)
	return pds
}

func TestOverrideDirectoryOverridesPDS(t *testing.T) {
	pds := unsupportedPDS(t)

	base := identity.NewMockDirectory()
	base.Insert(identity.Identity{
		DID:         syntax.DID("did:web:alice.example.com"),
		Handle:      syntax.Handle("alice.example.com"),
		AlsoKnownAs: []string{"at://alice.example.com"},
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: pds.URL},
		},
	})

	dir := NewOverrideDirectory(base, "pear.domain")
	dir.httpClient = pds.Client()

	ident, err := dir.LookupDID(t.Context(), syntax.DID("did:web:alice.example.com"))
	require.NoError(t, err)
	doc := ident.DIDDocument()
	require.Len(t, doc.Service, 1)
	require.Equal(t, "#atproto_pds", doc.Service[0].ID)
	require.Equal(t, "AtprotoPersonalDataServer", doc.Service[0].Type)
	require.Equal(t, "https://pear.domain", doc.Service[0].ServiceEndpoint)

	// The base directory's stored identity is untouched (cache-safety: the
	// override must never mutate what the base directory returns).
	original, err := base.LookupDID(t.Context(), syntax.DID("did:web:alice.example.com"))
	require.NoError(t, err)
	require.Equal(t, pds.URL, original.PDSEndpoint())
}

func TestOverrideDirectoryKeepsRealPDS(t *testing.T) {
	pds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(pds.Close)

	base := identity.NewMockDirectory()
	base.Insert(identity.Identity{
		DID:    syntax.DID("did:web:alice.example.com"),
		Handle: syntax.Handle("alice.example.com"),
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: pds.URL},
			"habitat":     {Type: "HabitatServer", URL: "https://other.example.com"},
		},
	})

	dir := NewOverrideDirectory(base, "pear.domain")
	dir.httpClient = pds.Client()

	ident, err := dir.LookupHandle(t.Context(), syntax.Handle("alice.example.com"))
	require.NoError(t, err)
	require.Equal(t, pds.URL, ident.PDSEndpoint())
	require.Equal(t, "https://other.example.com", ident.GetServiceEndpoint("habitat"))
}

func TestOverrideDirectoryLookupResolvesHandleDID(t *testing.T) {
	base := identity.NewMockDirectory()
	base.Insert(identity.Identity{
		DID:    syntax.DID("did:web:alice.example.com"),
		Handle: syntax.Handle("alice.example.com"),
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: "https://pds.example.com"},
		},
	})
	dir := NewOverrideDirectory(base, "pear.domain")
	dir.httpClient = &http.Client{Transport: noNetworkTransport{}}

	atid, err := syntax.ParseAtIdentifier("alice.example.com")
	require.NoError(t, err)
	ident, err := dir.Lookup(t.Context(), atid)
	require.NoError(t, err)
	require.Equal(t, syntax.DID("did:web:alice.example.com"), ident.DID)
	require.Equal(t, "https://pear.domain", ident.PDSEndpoint())
}

func TestOverrideDirectoryLookupIdentifierDID(t *testing.T) {
	base := identity.NewMockDirectory()
	base.Insert(identity.Identity{
		DID:    syntax.DID("did:web:alice.example.com"),
		Handle: syntax.Handle("alice.example.com"),
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: "https://pds.example.com"},
		},
	})
	dir := NewOverrideDirectory(base, "pear.domain")
	dir.httpClient = &http.Client{Transport: noNetworkTransport{}}

	ident, err := dir.LookupIdentifier(t.Context(), "did:web:alice.example.com")
	require.NoError(t, err)
	require.Equal(t, syntax.DID("did:web:alice.example.com"), ident.DID)
	require.Equal(t, "https://pear.domain", ident.PDSEndpoint())
}

func TestOverrideDirectoryLookupIdentifierUnknownHandle(t *testing.T) {
	dir := NewOverrideDirectory(identity.NewMockDirectory(), "pear.domain")

	_, err := dir.LookupIdentifier(t.Context(), "nobody.example.com")
	require.ErrorIs(t, err, identity.ErrHandleNotFound)
}

func TestOverrideDirectoryLookupIdentifierInvalid(t *testing.T) {
	dir := NewOverrideDirectory(identity.NewMockDirectory(), "pear.domain")

	_, err := dir.LookupIdentifier(t.Context(), "not an identifier")
	require.ErrorIs(t, err, identity.ErrInvalidHandle)
}

func TestOverrideDirectoryLookupEmailMintsAndOverrides(t *testing.T) {
	f := newEmailFixture(t)
	dir := NewOverrideDirectory(identity.NewMockDirectory(), "pear.domain")
	dir.httpClient = &http.Client{Transport: noNetworkTransport{}}
	dir.emailResolver = f.resolver

	ident, err := dir.LookupEmail(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.Equal(t, syntax.Handle("alice.acme.example.com"), ident.Handle)
	require.Equal(t, "https://pear.domain", ident.PDSEndpoint())
	// Minting alone must not enroll the identity in the org.
	require.Equal(t, 0, f.memberships(t))
}

func TestOverrideDirectoryLookupIdentifierEmail(t *testing.T) {
	f := newEmailFixture(t)
	dir := NewOverrideDirectory(identity.NewMockDirectory(), "pear.domain")
	dir.httpClient = &http.Client{Transport: noNetworkTransport{}}
	dir.emailResolver = f.resolver

	ident, err := dir.LookupIdentifier(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.Equal(t, syntax.Handle("alice.acme.example.com"), ident.Handle)
	require.Equal(t, "https://pear.domain", ident.PDSEndpoint())
}

func TestOverrideDirectoryLookupEmailUnknownDomain(t *testing.T) {
	f := newEmailFixture(t)
	dir := NewOverrideDirectory(identity.NewMockDirectory(), "pear.domain")
	dir.emailResolver = f.resolver

	_, err := dir.LookupEmail(t.Context(), "alice@other.com")
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
}

func TestOverrideDirectoryLookupEmailDisabled(t *testing.T) {
	dir := NewOverrideDirectory(identity.NewMockDirectory(), "pear.domain")

	_, err := dir.LookupEmail(t.Context(), "alice@acme.com")
	require.ErrorIs(t, err, identity.ErrInvalidHandle)
}

func TestOverrideDirectoryPurge(t *testing.T) {
	dir := NewOverrideDirectory(identity.NewMockDirectory(), "pear.domain")

	atid, err := syntax.ParseAtIdentifier("alice.example.com")
	require.NoError(t, err)
	require.NoError(t, dir.Purge(t.Context(), atid))
}
```

Note: `noNetworkTransport` (from `email_test.go`) fails every request, so `SupportsSpaces` reads every PDS as unsupported. `newEmailFixture`/`memberships` come from `email_test.go`. Both are in this package already.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/identity/ -run TestOverrideDirectory -v`

Expected: FAIL — `undefined: NewOverrideDirectory`.

- [ ] **Step 3: Implement `OverrideDirectory`**

Create `internal/identity/override_dir.go`:

```go
package identity

import (
	"context"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/httpx"
)

// OverrideDirectory resolves identities through a base directory and returns
// each one with its DID document overridden so its #atproto_pds service points
// at this habitat instance — unless the identity's real PDS already implements
// the atproto spaces protocol. It also resolves work emails (minting an
// identity on first sight) when an EmailResolver is configured.
type OverrideDirectory struct {
	base          identity.Directory
	emailResolver *EmailResolver
	domain        string
	httpClient    *http.Client
}

// NewOverrideDirectory constructs an OverrideDirectory over base. Identities
// whose real PDS doesn't support spaces get their #atproto_pds redirected to
// "https://" + domain. httpClient defaults to a fresh httpx client and is used
// to probe PDS spaces support.
func NewOverrideDirectory(base identity.Directory, domain string) *OverrideDirectory {
	return &OverrideDirectory{
		base:       base,
		domain:     domain,
		httpClient: httpx.NewClient(),
	}
}

// LookupDID implements identity.Directory.
func (d *OverrideDirectory) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	ident, err := d.base.LookupDID(ctx, did)
	if err != nil {
		return nil, err
	}
	return d.applyOverride(ctx, ident), nil
}

// LookupHandle implements identity.Directory.
func (d *OverrideDirectory) LookupHandle(ctx context.Context, handle syntax.Handle) (*identity.Identity, error) {
	ident, err := d.base.LookupHandle(ctx, handle)
	if err != nil {
		return nil, err
	}
	return d.applyOverride(ctx, ident), nil
}

// Lookup implements identity.Directory.
func (d *OverrideDirectory) Lookup(ctx context.Context, atid syntax.AtIdentifier) (*identity.Identity, error) {
	ident, err := d.base.Lookup(ctx, atid)
	if err != nil {
		return nil, err
	}
	return d.applyOverride(ctx, ident), nil
}

// Purge implements identity.Directory.
func (d *OverrideDirectory) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	return d.base.Purge(ctx, atid)
}

// LookupEmail resolves a work email, minting an identity on first sight, and
// applies the DID override to whatever it returns. It returns
// identity.ErrInvalidHandle when no EmailResolver is configured and
// identity.ErrDIDNotFound when the email's domain isn't mapped.
func (d *OverrideDirectory) LookupEmail(ctx context.Context, email emaildomain.Email) (*identity.Identity, error) {
	if d.emailResolver == nil {
		return nil, identity.ErrInvalidHandle
	}
	ident, err := d.emailResolver.ResolveEmailIdentity(ctx, email)
	if err != nil {
		return nil, err
	}
	return d.applyOverride(ctx, ident), nil
}

// LookupIdentifier resolves an identifier string as an at-identifier (DID or
// handle) or, failing that, as a work email. It returns
// identity.ErrInvalidHandle for input that is neither.
func (d *OverrideDirectory) LookupIdentifier(ctx context.Context, identifier string) (*identity.Identity, error) {
	if atid, err := syntax.ParseAtIdentifier(identifier); err == nil {
		return d.Lookup(ctx, atid)
	}
	if email, err := emaildomain.ParseEmail(identifier); err == nil {
		return d.LookupEmail(ctx, email)
	}
	return nil, identity.ErrInvalidHandle
}

// applyOverride returns ident, or a copy of it whose #atproto_pds service
// points at this habitat instance when the identity's real PDS doesn't support
// the spaces protocol. A fresh identity is returned rather than mutating the
// base directory's — possibly cached — one.
func (d *OverrideDirectory) applyOverride(ctx context.Context, ident *identity.Identity) *identity.Identity {
	if utils.SupportsSpaces(ctx, d.httpClient, ident) {
		return ident
	}
	return &identity.Identity{
		DID:         ident.DID,
		Handle:      ident.Handle,
		AlsoKnownAs: ident.AlsoKnownAs,
		Keys:        ident.Keys,
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {
				Type: "AtprotoPersonalDataServer",
				URL:  "https://" + d.domain,
			},
		},
	}
}
```

Note: this references `utils.SupportsSpaces`, so add the import:

```go
	"github.com/habitat-network/habitat/internal/utils"
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/identity/ -run TestOverrideDirectory -v`

Expected: PASS (all `TestOverrideDirectory*` tests).

- [ ] **Step 5: Run the full package tests (regression check)**

Run: `go test ./internal/identity/`

Expected: PASS (server/email tests unaffected).

- [ ] **Step 6: Commit**

```bash
git add internal/identity/override_dir.go internal/identity/override_dir_test.go
git commit -m "feat(identity): add OverrideDirectory with DID override and email lookups"
```

---

## Task 2: Rewire the server through `OverrideDirectory`

Move `WithClient`/`WithEmailResolver` onto `OverrideDirectory`, delete the duplicated server logic (`parseEmail`, `overriddenDidDoc`, and the `emailResolver`/`domain`/`httpClient` fields), and make the resolve handlers thin lookups. This task is atomic: the option functions and `Server.directory` type both flip in one change so the package stays green.

**Files:**
- Modify: `internal/identity/override_dir.go` (constructor gains `opts`, imports gain `utils`... `utils` is added in Step 1 via the `applyOverride` note; adjust the constructor and add the option functions)
- Modify: `internal/identity/server.go`
- Modify: `internal/identity/server_test.go` (`testResolveServer`)
- Modify: `internal/identity/email_test.go` (`emailServer`, `TestResolveIdentityEmailDisabled`)
- Test: `internal/identity/server_test.go`, `internal/identity/email_test.go`, `internal/identity/override_dir_test.go`

- [ ] **Step 1: Update `OverrideDirectory` to take options**

Edit `internal/identity/override_dir.go`:

- Add the `utils` import (also used by `applyOverride`):

```go
	"github.com/habitat-network/habitat/internal/utils"
```

- Replace the constructor `NewOverrideDirectory(base identity.Directory, domain string)` with an options-taking version, and append the two option functions after `LookupEmail` (or near the constructor — order is cosmetic):

```go
// NewOverrideDirectory constructs an OverrideDirectory over base, redirecting
// identities whose real PDS doesn't support spaces to serve from this habitat
// instance.
func NewOverrideDirectory(
	base identity.Directory,
	domain string,
	opts ...utils.Opt[OverrideDirectory],
) *OverrideDirectory {
	dir := utils.ResolveOptions(OverrideDirectory{
		base:       base,
		domain:     domain,
		httpClient: httpx.NewClient(),
	}, opts)
	return &dir
}

// WithClient sets the HTTP client used to probe whether an identity's PDS
// supports the atproto spaces protocol.
func WithClient(client *http.Client) utils.Opt[OverrideDirectory] {
	return func(d *OverrideDirectory) {
		d.httpClient = client
	}
}

// WithEmailResolver lets the directory resolve a work email in place of a
// handle, minting an identity on first sight (see EmailResolver). Without it,
// email identifiers are rejected.
func WithEmailResolver(r *EmailResolver) utils.Opt[OverrideDirectory] {
	return func(d *OverrideDirectory) {
		d.emailResolver = r
	}
}
```

- [ ] **Step 2: Rewrite `server.go`**

Replace the entire contents of `internal/identity/server.go` with:

```go
package identity

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/did"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/utils"
)

const HabitatHostHeader = "Habitat-Host"

// effectiveHost returns the Habitat-Host header value if present,
// otherwise falls back to the request's Host field.
func effectiveHost(r *http.Request) string {
	if h := r.Header.Get(HabitatHostHeader); h != "" {
		return h
	}
	return r.Host
}

// Server serves DID docs and handle --> did mappings.
// Does not serve the MintIdentity endpoint.
type Server struct {
	hive          hive.Hive
	directory     *OverrideDirectory
	validator     authn.RequestValidator
	orgStore      org.Store
	pdsForwarding *forwarding.PDSForwarding
}

// NewServer constructs the hive HTTP server. The validator is required to
// authenticate the caller for endpoints that mint things using the identity's
// signing key (e.g. com.atproto.server.getServiceAuth). Options configure the
// identity resolution directory, which serves DID docs with their PDS
// redirected here except when the identity's real PDS supports spaces.
func NewServer(
	hive hive.Hive,
	validator authn.RequestValidator,
	orgStore org.Store,
	pdsForwarding *forwarding.PDSForwarding,
	domain string,
	opts ...utils.Opt[OverrideDirectory],
) (*Server, error) {
	directory := NewOverrideDirectory(
		NewWrappedDirectory(hive, identity.DefaultDirectory()),
		domain,
		opts...,
	)
	return &Server{
		hive:          hive,
		directory:     directory,
		validator:     validator,
		orgStore:      orgStore,
		pdsForwarding: pdsForwarding,
	}, nil
}

// GetServiceAuth implements com.atproto.server.getServiceAuth for habitat-hosted
// identities. Habitat owns the signing key registered in the identity's did:web
// doc, so it (not the upstream PDS) is what can mint atproto-compatible service
// auth JWTs. Downstream services verify the token by resolving the DID and
// fetching the same signing key, with no changes needed on their end.
func (s *Server) GetServiceAuth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	aud := r.URL.Query().Get("aud")
	if aud == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required parameter: aud", nil)
		return
	}

	var ttl *time.Duration
	if expStr := r.URL.Query().Get("exp"); expStr != "" {
		expUnix, err := strconv.ParseInt(expStr, 10, 64)
		if err != nil {
			httpx.WriteInvalidRequest(ctx, w, "invalid exp", err)
			return
		}
		ttl = new(time.Until(time.Unix(expUnix, 0)))
	}

	var lxm *syntax.NSID
	if lxmStr := r.URL.Query().Get("lxm"); lxmStr != "" {
		parsed, ok := httpx.ParseNSIDInput(ctx, w, lxmStr, "lxm")
		if !ok {
			return
		}
		lxm = &parsed
	}

	privKey, err := s.hive.PrivateKeyForDID(ctx, credInfo.Subject)
	if errors.Is(err, identity.ErrDIDNotFound) {
		s.pdsForwarding.ServeHTTP(w, r)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("fetching signing key: %w", err))
		return
	}
	token, err := utils.ServiceAuthToken(privKey, credInfo.Subject, aud, lxm, ttl)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("signing service auth: %w", err))
		return
	}

	httpx.WriteJSON(ctx, w, struct {
		Token string `json:"token"`
	}{Token: token})
}

// For now, DIDs and handles are public. Eventually, we can make them private behind an
// auth boundary, to not leak info about who is in an org.

// Serve DID Doc ( satisfy /{did}/.well-known/did.json )
func (s *Server) ServeDIDDoc(w http.ResponseWriter, r *http.Request) {
	// Get the requested DID
	reqDID, err := syntax.ParseDID("did:web:" + effectiveHost(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	ident, err := s.hive.LookupDID(r.Context(), reqDID)
	// TODO: better status codes dependening on the identity.Err type
	if err != nil {
		http.NotFound(w, r)
		return
	}
	did.NewHandler(ident).ServeHTTP(w, r)
}

// Serve handle DID ( satisfy /{handle}/.well-known/atproto-did )
func (s *Server) ServeHandle(w http.ResponseWriter, r *http.Request) {
	handle, err := syntax.ParseHandle(effectiveHost(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	ident, err := s.hive.LookupHandle(r.Context(), handle)
	// TODO: better status codes dependening on the identity.Err type
	if err != nil {
		http.Error(
			w,
			"internal error",
			http.StatusInternalServerError,
		) // don't leak whether the DID exists or not
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(ident.DID.String()))
}

// ResolveDID implements com.atproto.identity.resolveDid.
func (s *Server) ResolveDID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	did, ok := httpx.ParseDIDInput(ctx, w, r.URL.Query().Get("did"), "did")
	if !ok {
		return
	}
	ident, err := s.directory.LookupDID(ctx, did)
	if errors.Is(err, identity.ErrDIDNotFound) {
		httpx.WriteError(ctx, w, "DidNotFound", "DID not found", http.StatusNotFound)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("resolving DID: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, atproto.IdentityResolveDid_Output{
		DidDoc: ident.DIDDocument(),
	})
}

// ResolveHandle implements com.atproto.identity.resolveHandle.
func (s *Server) ResolveHandle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	handleStr := r.URL.Query().Get("handle")
	if handleStr == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required parameter: handle", nil)
		return
	}
	// resolveHandle takes a handle; a DID reads as an invalid handle.
	if _, err := syntax.ParseDID(handleStr); err == nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid handle", nil)
		return
	}
	ident, err := s.directory.LookupIdentifier(ctx, handleStr)
	if errors.Is(err, identity.ErrDIDNotFound) {
		// an email whose domain isn't mapped reads as an unknown handle
		err = identity.ErrHandleNotFound
	}
	if errors.Is(err, identity.ErrHandleNotFound) {
		httpx.WriteError(ctx, w, "HandleNotFound", "handle not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, identity.ErrInvalidHandle) {
		httpx.WriteInvalidRequest(ctx, w, "invalid handle", err)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("resolving handle: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, atproto.IdentityResolveHandle_Output{
		Did: ident.DID.String(),
	})
}

// ResolveIdentity implements com.atproto.identity.resolveIdentity.
func (s *Server) ResolveIdentity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identifier := r.URL.Query().Get("identifier")
	if identifier == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required parameter: identifier", nil)
		return
	}
	ident, err := s.directory.LookupIdentifier(ctx, identifier)
	if errors.Is(err, identity.ErrDIDNotFound) {
		httpx.WriteError(ctx, w, "DidNotFound", "DID not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, identity.ErrHandleNotFound) {
		httpx.WriteError(ctx, w, "HandleNotFound", "handle not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, identity.ErrInvalidHandle) {
		httpx.WriteInvalidRequest(ctx, w, "invalid identifier", err)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("resolving identity: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, atproto.IdentityDefs_IdentityInfo{
		Did:    ident.DID.String(),
		Handle: ident.Handle.String(),
		DidDoc: ident.DIDDocument(),
	})
}
```

- [ ] **Step 3: Update the test fixture in `server_test.go`**

Replace `testResolveServer` in `internal/identity/server_test.go` with:

```go
func testResolveServer(t *testing.T) *Server {
	t.Helper()
	pds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(pds.Close)

	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID:         syntax.DID("did:web:alice.example.com"),
		Handle:      syntax.Handle("alice.example.com"),
		AlsoKnownAs: []string{"at://alice.example.com"},
		Services: map[string]identity.ServiceEndpoint{
			"atproto": {
				Type: "AtprotoPersonalDataServer",
				URL:  pds.URL,
			},
		},
	})
	return &Server{
		directory: NewOverrideDirectory(dir, "pear.domain", WithClient(pds.Client())),
	}
}
```

(That `"atproto"` service key is intentional — it mirrors the current fixture and the override replaces `Services` wholesale anyway, so the resulting DID doc is what the tests assert: a single `#atproto_pds` service at `https://pear.domain`.)

- [ ] **Step 4: Update the email test fixture**

In `internal/identity/email_test.go`, replace `emailServer` with:

```go
func emailServer(f emailFixture, opts ...func(*Server)) *Server {
	s := &Server{
		hive: f.hive,
		directory: NewOverrideDirectory(
			NewWrappedDirectory(f.hive, identity.NewMockDirectory()),
			"pear.domain",
			WithClient(&http.Client{Transport: noNetworkTransport{}}),
			WithEmailResolver(f.resolver),
		),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
```

In the same file, update `TestResolveIdentityEmailDisabled` so disabling email is expressed through the directory:

```go
func TestResolveIdentityEmailDisabled(t *testing.T) {
	f := newEmailFixture(t)
	s := emailServer(f, func(s *Server) { s.directory.emailResolver = nil })
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

- [ ] **Step 5: Run the package tests**

Run: `go test ./internal/identity/`

Expected: PASS (all existing server/email/wrapped-dir/override tests).

- [ ] **Step 6: Commit**

```bash
git add internal/identity/override_dir.go internal/identity/server.go internal/identity/server_test.go internal/identity/email_test.go
git commit -m "refactor(identity): serve identity resolution through OverrideDirectory"
```

---

## Task 3: Verify the whole repository

Ensure the callers (`cmd/pear`, the integration module) still build, the identity tests pass, and lint is clean.

- [ ] **Step 1: Build the root Go module**

Run: `go build ./...`

Expected: no output, exit 0.

- [ ] **Step 2: Build the integration module**

Run: `go build ./...` with `workdir: integration`

Expected: no output, exit 0.

- [ ] **Step 3: Run all identity tests**

Run: `go test ./internal/identity/...`

Expected: PASS.

- [ ] **Step 4: Lint**

Run: `proto run golangci-lint run ./internal/identity/...`

Expected: no findings.

- [ ] **Step 5: Commit any stray cleanup**

If Step 4 surfaced only unrelated/no issues and no files changed, stop here. If `golangci-lint` or `gofmt` flagged the changed files, fix and commit:

```bash
git add internal/identity/
git commit -m "chore(identity): lint fixes"
```

---

## Self-Review Notes

- **Spec coverage:** The spec's OverrideDirectory (.go), email/identifier methods, applyOverride copy-semantics, server rewiring, and test updates all map to Tasks 1–2; Task 3 covers the spec's verification section (`go test`, lint, build, integration module).
- **Behavioral parity:** Error mappings preserved — `ResolveHandle` maps unmapped email domains to `HandleNotFound` and rejects DIDs with 400; `ResolveIdentity` keeps `DidNotFound` vs `HandleNotFound` distinction; disabled resolver still yields 400. The override document (single `#atproto_pds` service, keys + alsoKnownAs preserved) is byte-equivalent in intent.
- **Type consistency:** `LookupDID`/`LookupHandle`/`Lookup`/`Purge` satisfy `identity.Directory`; `NewOverrideDirectory` returns `*OverrideDirectory` with `utils.Opt[OverrideDirectory]` options; `Server.directory` is `*OverrideDirectory`. `identity.ErrInvalidHandle`, `identity.ErrDIDNotFound`, `identity.ErrHandleNotFound` are indigo sentinels referenced consistently throughout.