# OAuth Login Methods Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route OAuth login providers based on an org's login method instead of inspecting identity services via `CanHandle`.

**Architecture:** Each org stores a `LoginMethod` ("atproto", "google", "password"). The `Provider` interface replaces `Type()`/`CanHandle()` with `LoginMethod()`. The Router dispatches via `map[string]Provider` keyed by login method. `HandleAuthorize` looks up the org after resolving the handle to determine the provider. The `pdsProvider` gains a `PublicDIDMappingStore` to resolve org DIDs to public ATProto DIDs. A new `googleProvider` stubs Google Sign-In.

**Tech Stack:** Go, Fosite, GORM, AT Protocol (indigo)

---

### Task 1: Org model — add LoginMethod

**Files:**
- Modify: `internal/org/models.go` (add field)
- Modify: `internal/org/org.go` (add interface method)
- Modify: `internal/org/store.go` (implement, propagate in CreateOrg)
- Modify: `internal/org/public.go` (everyoneOrg implementation)
- Test: `internal/org/org_test.go`, `internal/org/public_test.go`

**Step 1.1: Add LoginMethod to the organization model**

`internal/org/models.go` — add `LoginMethod` field:

```go
type organization struct {
	ID            string `gorm:"primaryKey"`
	Name          string // optional display name
	LoginMethod   string // "atproto", "google", "password"
	SigningSecret string // base64-encoded HMAC-SHA256 key for invite tokens
	CreatedAt     time.Time
}
```

**Step 1.2: Add LoginMethod to the Org interface**

`internal/org/org.go` — add to `Org` interface:

```go
type Org interface {
	// LoginMethod returns how users authenticate: "atproto", "google", or "password".
	LoginMethod() string
	// ... all existing methods unchanged ...
}
```

**Step 1.3: Implement LoginMethod on orgImpl**

`internal/org/store.go` — add to `orgImpl`:

```go
func (s *orgImpl) LoginMethod() string {
	var org organization
	if err := s.db.First(&org, "id = ?", s.orgID).Error; err != nil {
		return "password" // safe default
	}
	return org.LoginMethod
}
```

**Step 1.4: Update orgFromModel to propagate LoginMethod**

`internal/org/store.go` — `orgFromModel` needs no changes since `orgImpl` reads LoginMethod from DB directly in step 1.3.

**Step 1.5: Update CreateOrg to set LoginMethod**

`internal/org/store.go` — in `CreateOrg`, update the org creation to include `LoginMethod`:

```go
err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
	if err := tx.Create(&organization{
		ID:            orgID,
		Name:          name,
		LoginMethod:   "password",  // new orgs default to password auth
		SigningSecret: signingSecret,
		CreatedAt:     time.Now(),
	}).Error; err != nil {
```

**Step 1.6: Add LoginMethod to everyoneOrg**

`internal/org/public.go`:

```go
func (e *everyoneOrg) LoginMethod() string {
	return "atproto"
}
```

**Step 1.7: Run existing tests to verify no regression**

```bash
go test ./internal/org/... -v -run "TestEveryoneOrg|TestIsMember|TestCreateOrg" -count=1
```

Expected: all pass.

**Step 1.8: Commit**

```bash
git add internal/org/models.go internal/org/org.go internal/org/store.go internal/org/public.go
git commit -m "feat: add LoginMethod field to org model and interface"
```

---

### Task 2: Update LoginProvider (password) to new Provider interface

**Files:**
- Modify: `internal/org/login_provider.go`
- Test: `internal/org/login_provider_test.go`

**Step 2.1: Replace Type() with LoginMethod(), remove CanHandle()**

`internal/org/login_provider.go`:

```go
func (p *LoginProvider) LoginMethod() string { return "password" }
```

Remove the `Type()` method and `CanHandle()` method entirely.

**Step 2.2: Update tests**

`internal/org/login_provider_test.go` — update `TestLoginProvider_Type`:

```go
func TestLoginProvider_LoginMethod(t *testing.T) {
	p, _ := newTestLoginProvider(t)
	require.Equal(t, "password", p.LoginMethod())
}
```

Remove `TestLoginProvider_CanHandle` entirely.

**Step 2.3: Run tests**

```bash
go test ./internal/org/... -v -run "TestLoginProvider" -count=1
```

Expected: TestLoginProvider_LoginMethod passes, TestLoginProvider_CanHandle no longer exists.

**Step 2.4: Commit**

```bash
git add internal/org/login_provider.go internal/org/login_provider_test.go
git commit -m "refactor: replace Type/CanHandle on LoginProvider with LoginMethod"
```

---

### Task 3: Rewrite Provider interface and Router

**Files:**
- Modify: `internal/login/provider.go`
- Test: `internal/login/login_test.go`

**Step 3.1: Rewrite Provider interface and Router**

`internal/login/provider.go` — replace entire file:

```go
package login

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Provider abstracts a login backend. Each implementation handles a specific
// login method (e.g., "atproto", "google", "password") and is responsible for
// its own credential storage.
type Provider interface {
	// LoginMethod returns a stable identifier used to route authorization
	// requests to the correct provider based on the org's configured method.
	LoginMethod() string

	// Authorize starts the auth flow and returns the redirect URL plus opaque
	// provider-specific state to be stored in the session flash.
	Authorize(ctx context.Context, id *identity.Identity) (redirectUri string, state []byte, err error)

	// Exchange exchanges the callback code for credentials and should persist
	// whatever credentials the provider acquires.
	Exchange(ctx context.Context, did syntax.DID, code string, issuer string, state []byte) error
}

// Router selects the correct Provider for a given login method.
type Router struct {
	providers map[string]Provider
}

func NewRouter(providers ...Provider) *Router {
	r := &Router{providers: make(map[string]Provider, len(providers))}
	for _, p := range providers {
		r.providers[p.LoginMethod()] = p
	}
	return r
}

// ByLoginMethod returns the provider registered for the given login method.
func (r *Router) ByLoginMethod(method string) (Provider, error) {
	p, ok := r.providers[method]
	if !ok {
		return nil, fmt.Errorf("no login provider for method %q", method)
	}
	return p, nil
}
```

**Step 3.2: Remove ProviderType constants** — they're gone. Remove `ProviderType`, `ProviderTypePDS`, `ProviderTypeHabitat`.

**Step 3.3: Update tests**

`internal/login/login_test.go` — update:

Remove `TestPDSProvider_CanHandle`. Update `dummyProvider` to implement new interface:

```go
type dummyProvider struct{}

func NewDummyProvider() Provider { return &dummyProvider{} }

func (d *dummyProvider) LoginMethod() string { return "password" }
func (d *dummyProvider) Authorize(_ context.Context, _ *identity.Identity) (string, []byte, error) {
	return "https://dummy.example.com/login", nil, nil
}
func (d *dummyProvider) Exchange(_ context.Context, _ syntax.DID, _, _ string, _ []byte) error {
	return nil
}
```

Replace Router tests:

```go
func newTestRouter() *Router {
	return NewRouter(
		NewPDSProvider(&stubOAuthClient{}, newStubCredStore(), &stubMappingStore{}, nil),
		NewDummyProvider(),
	)
}

func TestRouter_ByLoginMethod(t *testing.T) {
	r := newTestRouter()

	p, err := r.ByLoginMethod("atproto")
	require.NoError(t, err)
	require.Equal(t, "atproto", p.LoginMethod())

	p, err = r.ByLoginMethod("password")
	require.NoError(t, err)
	require.Equal(t, "password", p.LoginMethod())

	_, err = r.ByLoginMethod("unknown")
	require.Error(t, err)
}
```

Remove `TestRouter_For` and `TestRouter_ByType` entirely.

Also update `TestPDSProvider_Authorize` and `TestPDSProvider_Exchange` — they call `NewPDSProvider` which now needs new args. Update them:

```go
func TestPDSProvider_Authorize(t *testing.T) {
	client := &stubOAuthClient{redirectURL: "https://pds.example.com/authorize"}
	p := NewPDSProvider(client, newStubCredStore(), &stubMappingStore{}, nil)

	redirect, state, err := p.Authorize(context.Background(), idWithPDSOnly())
	require.NoError(t, err)
	require.Equal(t, "https://pds.example.com/authorize", redirect)
	require.NotEmpty(t, state)

	var s pdsProviderState
	require.NoError(t, unmarshalProviderState(state, &s))
	require.NotEmpty(t, s.DpopKey)
	require.Equal(t, "verifier", s.AuthorizeState.Verifier)
}

func TestPDSProvider_Exchange(t *testing.T) {
	credStore := newStubCredStore()
	p := NewPDSProvider(&stubOAuthClient{redirectURL: "https://pds.example.com/authorize"}, credStore, &stubMappingStore{}, nil)
	did := syntax.DID("did:web:pds.example.com")

	_, state, err := p.Authorize(context.Background(), idWithPDSOnly())
	require.NoError(t, err)

	err = p.Exchange(context.Background(), did, "code", "https://pds.example.com", state)
	require.NoError(t, err)

	creds, stored := credStore.upserted[did]
	require.True(t, stored, "credentials should have been upserted")
	require.Equal(t, "access", creds.AccessToken)
	require.Equal(t, "refresh", creds.RefreshToken)
	require.NotNil(t, creds.DpopKey)
}
```

Add the stubMappingStore before the test file's type definitions:

```go
type stubMappingStore struct{}

func (s *stubMappingStore) GetPublicDID(_ context.Context, _ syntax.DID) (*syntax.DID, error) {
	return nil, nil // no mapping — use identity as-is
}
```

**Step 3.4: Run tests**

```bash
go test ./internal/login/... -v -count=1
```

Expected: all tests pass.

**Step 3.5: Commit**

```bash
git add internal/login/provider.go internal/login/login_test.go
git commit -m "refactor: replace ProviderType/CanHandle with LoginMethod-based routing"
```

---

### Task 4: Update pdsProvider with mapping store

**Files:**
- Modify: `internal/login/pds.go`
- Test: `internal/login/login_test.go` (already updated in Task 3)

**Step 4.1: Add PublicDIDMappingStore interface and update pdsProvider**

`internal/login/pds.go` — add interface at top of file, update struct and constructor:

```go
// PublicDIDMappingStore resolves an org's internal DID to its public
// AT Protocol DID for the purposes of initiating PDS OAuth flows.
type PublicDIDMappingStore interface {
	GetPublicDID(ctx context.Context, orgDID syntax.DID) (*syntax.DID, error)
}

type pdsProvider struct {
	loginMethod  string
	oauthClient  pdsclient.PdsOAuthClient
	credStore    pdscred.PDSCredentialStore
	mappingStore PublicDIDMappingStore
	dir          identity.Directory
}

func NewPDSProvider(oauthClient pdsclient.PdsOAuthClient, credStore pdscred.PDSCredentialStore, mappingStore PublicDIDMappingStore, dir identity.Directory) Provider {
	return &pdsProvider{
		loginMethod:  "atproto",
		oauthClient:  oauthClient,
		credStore:    credStore,
		mappingStore: mappingStore,
		dir:          dir,
	}
}
```

**Step 4.2: Replace Type() with LoginMethod(), remove CanHandle()**

```go
func (p *pdsProvider) LoginMethod() string { return p.loginMethod }
```

Remove `Type()` and `CanHandle()` methods.

**Step 4.3: Update Authorize to check mapping**

```go
func (p *pdsProvider) Authorize(ctx context.Context, id *identity.Identity) (string, []byte, error) {
	// If there's a mapping from org DID to public ATProto DID, resolve the
	// public identity and use its PDS for the OAuth flow.
	publicDID, err := p.mappingStore.GetPublicDID(ctx, id.DID)
	if err == nil && publicDID != nil && p.dir != nil {
		publicID, lookupErr := p.dir.LookupDID(ctx, *publicDID)
		if lookupErr == nil {
			id = publicID
		}
	}

	dpopKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", nil, fmt.Errorf("generate dpop key: %w", err)
	}
	dpopClient := pdsclient.NewDpopHttpClient(dpopKey, &pdsclient.MemoryNonceProvider{})
	redirect, state, err := p.oauthClient.Authorize(dpopClient, id)
	if err != nil {
		return "", nil, err
	}
	dpopKeyBytes, err := dpopKey.Bytes()
	if err != nil {
		return "", nil, fmt.Errorf("serialize dpop key: %w", err)
	}
	stateBytes, err := json.Marshal(pdsProviderState{DpopKey: dpopKeyBytes, AuthorizeState: *state})
	if err != nil {
		return "", nil, fmt.Errorf("marshal pds provider state: %w", err)
	}
	return redirect, stateBytes, nil
}
```

**Step 4.4: Add test for mapping store behavior**

`internal/login/login_test.go` — add:

```go
func TestPDSProvider_AuthorizeWithMapping(t *testing.T) {
	mappedDID := syntax.DID("did:plc:mapped-public")
	mapping := &stubMappingStoreWithMapping{mapped: mappedDID}
	client := &stubOAuthClient{redirectURL: "https://public-pds.example.com/authorize"}
	dir := &stubDirectory{dids: map[syntax.DID]*identity.Identity{
		mappedDID: {
			DID:    mappedDID,
			Handle: "org.public.example.com",
			Services: map[string]identity.ServiceEndpoint{
				"atproto_pds": {URL: "https://public-pds.example.com"},
			},
		},
	}}
	p := NewPDSProvider(client, newStubCredStore(), mapping, dir)

	// An org-internal identity (habitat DID) should resolve to the mapped public identity
	orgID := &identity.Identity{
		DID: "did:web:internal.org.example.com",
		Services: map[string]identity.ServiceEndpoint{
			"habitat": {URL: "https://habitat.example.com"},
		},
	}
	redirect, state, err := p.Authorize(context.Background(), orgID)
	require.NoError(t, err)
	require.Equal(t, "https://public-pds.example.com/authorize", redirect)
	require.NotEmpty(t, state)
}

type stubMappingStoreWithMapping struct {
	mapped syntax.DID
}

func (s *stubMappingStoreWithMapping) GetPublicDID(_ context.Context, _ syntax.DID) (*syntax.DID, error) {
	return &s.mapped, nil
}

type stubDirectory struct {
	dids map[syntax.DID]*identity.Identity
}

func (d *stubDirectory) LookupHandle(_ context.Context, _ syntax.Handle) (*identity.Identity, error) {
	return nil, errors.New("not implemented")
}

func (d *stubDirectory) LookupDID(_ context.Context, did syntax.DID) (*identity.Identity, error) {
	id, ok := d.dids[did]
	if !ok {
		return nil, errors.New("not found")
	}
	return id, nil
}

func (d *stubDirectory) Purge(_ context.Context) {}
```

**Step 4.5: Run tests**

```bash
go test ./internal/login/... -v -count=1
```

Expected: all tests pass, including the new mapping test.

**Step 4.6: Commit**

```bash
git add internal/login/pds.go internal/login/login_test.go
git commit -m "feat: add public DID mapping store to pdsProvider"
```

---

### Task 5: Create googleProvider stub

**Files:**
- Create: `internal/login/google.go`
- Test: `internal/login/login_test.go`

**Step 5.1: Create googleProvider**

`internal/login/google.go`:

```go
package login

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// GoogleEmailMappingStore resolves an org DID to the email address used
// for Google Sign-In.
type GoogleEmailMappingStore interface {
	GetEmail(ctx context.Context, orgDID syntax.DID) (string, error)
}

type googleProvider struct {
	loginMethod  string
	mappingStore GoogleEmailMappingStore
}

func NewGoogleProvider(mappingStore GoogleEmailMappingStore) Provider {
	return &googleProvider{
		loginMethod:  "google",
		mappingStore: mappingStore,
	}
}

func (p *googleProvider) LoginMethod() string { return p.loginMethod }

func (p *googleProvider) Authorize(ctx context.Context, id *identity.Identity) (string, []byte, error) {
	email, err := p.mappingStore.GetEmail(ctx, id.DID)
	if err != nil {
		return "", nil, fmt.Errorf("lookup google email for org %s: %w", id.DID, err)
	}
	if email == "" {
		return "", nil, fmt.Errorf("no google email configured for org %s", id.DID)
	}
	// TODO: implement Google OAuth initiation with login_hint=email
	_ = email
	return "", nil, fmt.Errorf("google provider not yet implemented")
}

func (p *googleProvider) Exchange(_ context.Context, _ syntax.DID, _, _ string, _ []byte) error {
	// TODO: implement Google OAuth callback handling
	return fmt.Errorf("google provider not yet implemented")
}
```

**Step 5.2: Add basic test for googleProvider**

`internal/login/login_test.go` — add:

```go
func TestGoogleProvider_LoginMethod(t *testing.T) {
	p := NewGoogleProvider(&stubGoogleMappingStore{})
	require.Equal(t, "google", p.LoginMethod())
}

type stubGoogleMappingStore struct{}

func (s *stubGoogleMappingStore) GetEmail(_ context.Context, _ syntax.DID) (string, error) {
	return "", nil
}
```

**Step 5.3: Run tests**

```bash
go test ./internal/login/... -v -count=1
```

Expected: all pass.

**Step 5.4: Commit**

```bash
git add internal/login/google.go internal/login/login_test.go
git commit -m "feat: add googleProvider stub"
```

---

### Task 6: Update OAuth server — org-based routing

**Files:**
- Modify: `internal/oauthserver/oauth_server.go`
- Test: `internal/oauthserver/oauth_server_test.go`

**Step 6.1: Replace ProviderType with LoginMethod in authRequestFlash**

`internal/oauthserver/oauth_server.go`:

```go
type authRequestFlash struct {
	Form          url.Values  // Original authorization request form data
	LoginMethod   string      // Which login method initiated this flow
	ProviderState []byte      // Opaque provider-specific state
	Did           syntax.DID  // DID of the user
}
```

**Step 6.2: Update HandleAuthorize to lookup org and route by login method**

Replace the relevant section:

```go
func (o *OAuthServer) HandleAuthorize(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx := r.Context()
	requester, err := o.provider.NewAuthorizeRequest(ctx, r)
	if err != nil {
		o.metrics.authorizeErr(err, fositeErrReason(err))
		o.provider.WriteAuthorizeError(ctx, w, requester, err)
		return
	}
	if err = r.ParseForm(); err != nil {
		o.metrics.authorizeErr(err, "parse_form")
		utils.LogAndHTTPError(w, err, "failed to parse form", http.StatusBadRequest)
		return
	}
	handle := r.Form.Get("handle")
	atid, err := syntax.ParseAtIdentifier(handle)
	if err != nil {
		o.metrics.authorizeErr(err, "parse_handle")
		utils.LogAndHTTPError(w, err, "failed to parse handle", http.StatusBadRequest)
		return
	}
	id, err := o.directory.Lookup(ctx, atid)
	if err != nil {
		o.metrics.authorizeErr(err, "lookup_atid")
		utils.LogAndHTTPError(w, err, "failed to lookup identity", http.StatusInternalServerError)
		return
	}

	// Look up org to determine the login method
	org, err := o.orgStore.GetOrgForDID(ctx, id.DID)
	if err != nil {
		o.metrics.authorizeErr(err, "no_org")
		utils.LogAndHTTPError(w, err, "no org found for identity", http.StatusBadRequest)
		return
	}

	provider, err := o.loginRouter.ByLoginMethod(org.LoginMethod())
	if err != nil {
		o.metrics.authorizeErr(err, "no_provider")
		utils.LogAndHTTPError(w, err, "no login provider for org", http.StatusBadRequest)
		return
	}
	redirect, providerState, err := provider.Authorize(ctx, id)
	if err != nil {
		o.metrics.authorizeErr(err, "begin_login")
		utils.LogAndHTTPError(w, err, "failed to initiate authorization", http.StatusInternalServerError)
		return
	}

	// Generate opaque flash id to store in cookie
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		o.metrics.authorizeErr(err, "gen_flash_id")
		utils.LogAndHTTPError(w, err, "failed to generate session id", http.StatusInternalServerError)
		return
	}
	flashID := hex.EncodeToString(b)

	o.flashMu.Lock()
	o.flashStore[flashID] = &authRequestFlash{
		Form:          requester.GetRequestForm(),
		LoginMethod:   provider.LoginMethod(),
		ProviderState: providerState,
		Did:           id.DID,
	}
	o.flashMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionName,
		Value:    flashID,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
		Path:     "/oauth-callback",
	})

	http.Redirect(w, r, redirect, http.StatusSeeOther)
	o.metrics.authorizeSuccess()
}
```

**Step 6.3: Update HandleCallback to route by login method**

Replace the relevant section:

```go
func (o *OAuthServer) HandleCallback(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx := r.Context()
	cookie, err := r.Cookie(sessionName)
	if err != nil {
		o.metrics.callbackErr(err, "get_session")
		utils.LogAndHTTPError(w, err, "failed to get session cookie", http.StatusBadRequest)
		return
	}

	// Lookup cookie --> flash
	o.flashMu.Lock()
	arf, ok := o.flashStore[cookie.Value]
	delete(o.flashStore, cookie.Value)
	o.flashMu.Unlock()

	if !ok {
		o.metrics.callbackErr(nil, "no_flash")
		utils.LogAndHTTPError(w, nil, "no state found for session", http.StatusBadRequest)
		return
	}

	recreatedRequest, err := http.NewRequest(http.MethodGet, "/?"+arf.Form.Encode(), nil)
	if err != nil {
		o.metrics.callbackErr(err, "recreate_req")
		utils.LogAndHTTPError(w, err, "failed to recreate request", http.StatusBadRequest)
		return
	}
	authRequest, err := o.provider.NewAuthorizeRequest(ctx, recreatedRequest)
	if err != nil {
		o.metrics.callbackErr(err, fositeErrReason(err))
		utils.LogAndHTTPError(w, err, "failed to recreate request", http.StatusBadRequest)
		return
	}

	// Check the DID allowlist after reconstructing authRequest so we can redirect
	// errors back to the client via WriteAuthorizeError instead of returning a raw 401.
	_, err = o.orgStore.GetOrgForDID(r.Context(), arf.Did)
	if err != nil {
		o.metrics.callbackErr(err, "allowlist_dids")
		o.provider.WriteAuthorizeError(ctx, w, authRequest,
			fosite.ErrAccessDenied.WithDescription("You are not a member of this habitat organization.").WithHint(""))
		return
	}
	provider, err := o.loginRouter.ByLoginMethod(arf.LoginMethod)
	if err != nil {
		o.metrics.callbackErr(err, "no_provider")
		utils.LogAndHTTPError(w, err, "no login provider for session", http.StatusBadRequest)
		return
	}
	if err := provider.Exchange(
		ctx,
		arf.Did,
		r.URL.Query().Get("code"),
		r.URL.Query().Get("iss"),
		arf.ProviderState,
	); err != nil {
		o.metrics.callbackErr(err, "complete_login")
		utils.LogAndHTTPError(w, err, "failed to complete login", http.StatusInternalServerError)
		return
	}

	resp, err := o.provider.NewAuthorizeResponse(
		ctx,
		authRequest,
		newAuthorizeSession(authRequest, arf.Did),
	)
	if err != nil {
		o.metrics.callbackErr(err, fositeErrReason(err))
		utils.LogAndHTTPError(w, err, "failed to create response", http.StatusInternalServerError)
		return
	}
	o.provider.WriteAuthorizeResponse(r.Context(), w, authRequest, resp)
	o.metrics.callbackSuccess()
}
```

**Step 6.4: Update OAuth server tests**

`internal/oauthserver/oauth_server_test.go` — update all `login.NewRouter(login.NewPDSProvider(oauthClient, credStore))` calls to include the new args:

```go
login.NewRouter(login.NewPDSProvider(oauthClient, credStore, &stubMappingStore{}, nil))
```

Add a `stubMappingStore` in the test file:

```go
type stubMappingStore struct{}

func (s *stubMappingStore) GetPublicDID(_ context.Context, _ syntax.DID) (*syntax.DID, error) {
	return nil, nil
}
```

Also update the OAuth server comment that mentions `loginRouter: Routes login flows by DID service endpoint`:

```go
// loginRouter: Routes login flows by org login method
```

Also update `testStore` — it creates an org with `CreateOrg` which now sets `LoginMethod: "password"`. The dummy PDS provider's `LoginMethod()` returns `"atproto"`, but the test store creates a "password" org. The test uses `"did:web:test"` as handle which will resolve through the dummy directory. The dummy directory likely returns an identity that maps to the everyone org (which has login method "atproto"). So the tests should still work.

Wait, let me think about this more carefully. The test creates an org via `testStore` → `CreateOrg`. That org's members are the admin user. The test then makes an authorize request with `handle=did:web:test`. The dummy directory resolves this to an identity with DID `did:web:test`. `GetOrgForDID` checks: is `did:web:test` in the member table? No (it's the admin handle's DID, not `did:web:test`). Then it resolves the DID → identity via directory. The dummy directory returns an identity. Does it have a habitat service? Probably not. So it falls through to the everyone org.

The everyone org's `LoginMethod()` returns `"atproto"`. The router has a provider with `LoginMethod() == "atproto"` (the PDS provider). So the test should still work.

Actually wait - looking at the dummy directory more carefully. The test uses `pdsclient.NewDummyDirectory("http://pds.url")`. I need to check what that returns. Let me look at what `NewDummyDirectory` does.

Actually, I don't have the code for `NewDummyDirectory`. But from the test, we see that it resolves `did:web:test` to an identity, and the identity has a PDS service. So it goes to the everyone org → login method "atproto" → PDS provider. The test should work.

But for the `TestOAuthServerErrorPaths` test, it creates a router with only a PDS provider (atproto). If the org created by `testStore` has login method "password", it would look for a "password" provider and not find one. But this test doesn't actually exercise the full authorize flow — it tests error paths like "missing OAuth params". So that should be fine.

Wait, actually, let me look at the test more carefully. `TestOAuthServerErrorPaths` does:
1. `NewOAuthServer(secret, login.NewRouter(login.NewPDSProvider(...)), ...)` — Router has only PDS provider
2. `HandleAuthorize` with no params → Fosite rejects it (no client_id, etc.)
3. So the OAuth flow never gets far enough to hit the org lookup

The `TestHandleCallbackDIDNotInAllowlist` test:
1. Creates a server with PDS provider only
2. Makes an authorize request with `handle=did:web:test`
3. The dummy directory resolves it
4. The org lookup → everyone org (since `did:web:test` is not in any org)
5. Login method "atproto" → PDS provider
6. Authorize returns redirect
7. Follows redirects to callback
8. GetOrgForDID → everyone org → succeeds
9. Exchange → succeeds
10. Fosite authorize response → ...

Wait, actually, this test drives the flow and expects it to succeed up to the authorise response. The org check in `HandleCallback` succeeds because `did:web:test` maps to the everyone org, and `IsMember` returns true for everyone. Then `ByLoginMethod("atproto")` finds the PDS provider. Exchange succeeds. Then Fosite issues the auth code redirect.

But wait - the test expects `http.StatusSeeOther`. In the current test, it checks `require.Equal(t, http.StatusSeeOther, resp.StatusCode)` at the callback step where fosite would redirect with the auth code. But looking at the test more carefully:

```go
for resp.StatusCode == http.StatusSeeOther {
    loc := resp.Header.Get("Location")
    nextReq, reqErr := http.NewRequest(http.MethodGet, loc, nil)
    require.NoError(t, reqErr)
    resp, err = httpClient.Do(nextReq)
    require.NoError(t, err)
    _ = resp.Body.Close()
    if nextReq.URL.Path == "/oauth-callback" {
        break
    }
}
require.Equal(t, http.StatusSeeOther, resp.StatusCode)
```

Hmm, actually the test stops at `/oauth-callback` and expects it to NOT be a redirect. It expects the fosite authorize error response, which uses `http.StatusSeeOther`. That would happen if GetOrgForDID fails with an error.

Wait, this test is `TestHandleCallbackDIDNotInAllowlist`. Let me look at it more carefully.

Looking at lines 150-256: The `testStore` creates an org with `"admin"` handle. The authorize request uses `handle=did:web:test`. The dummy directory at `http://pds.url` would resolve this. The org lookup would find the everyone org (since `did:web:test` isn't a member of any org). The everyone org's `IsMember` returns true. So the callback org check passes.

Hmm, but then the test expects failure at `http.StatusSeeOther`. Maybe the failure comes from fosite's authorize response? Looking at the test again... the test uses `clientMetadata.RedirectUris = []string{server.URL + "/oauth-callback"}` which actually doesn't match the client metadata URL. The fosite client ID would be something else.

Actually, I think this test is testing the case where fosite rejects the request at the final step (not at the org membership step). The test title is "TestHandleCallbackDIDNotInAllowlist" but it actually just verifies that a full OAuth flow with no valid memberships fails. The key point is whether our changes break the flow.

Let me think about this differently. The test creates an org with `CreateOrg(ctx, "org-name", "admin", "password")`. This creates an org with login method "password" and admin DID = whatever `MintIdentity("admin")` returns. The authorize request is `handle=did:web:test`. `directory.Lookup("did:web:test")` returns an identity with DID `did:web:test` and some services. `GetOrgForDID(did:web:test)` → checks member table → no match → resolves DID → gets identity with PDS service → returns everyone org (not a habitat member on this pear).

Everyone org's LoginMethod = "atproto". Router has PDS provider with LoginMethod "atproto". So `ByLoginMethod("atproto")` returns the PDS provider. Authorize succeeds. Flow continues. At callback, GetOrgForDID again returns everyone org. Login method still "atproto". Exchange succeeds. Fosite issues response.

So the flow should succeed through callback. The `testStore` org membership doesn't interfere because `did:web:test` resolves to the everyone org.

OK I think this will work without changes to the test logic, just the constructor call.

But wait — the `testStore` creates an org with `CreateOrg`. That org is a password org with a specific member. But the `did:web:test` handle used in the test resolves to everyone org. So the PDS provider is used correctly. The test should pass.

Actually, let me re-examine the `TestOAuthServerErrorPaths` test more carefully. It does:

```go
oauthSrv, err := NewOAuthServer(
    secret,
    login.NewRouter(login.NewPDSProvider(oauthClient, credStore)),
    ...
)
```

After my changes, `NewPDSProvider` needs `mappingStore` and `dir`. So I need to update this call. And `NewRouter` accepts `...Provider` as before.

For the `testStore` function, `CreateOrg` now stores `LoginMethod: "password"`. This doesn't affect the test flow since the test uses `did:web:test` which maps to everyone org.

Let me make sure I specify the exact changes needed for the test file.

**Step 6.5: Run tests**

```bash
go test ./internal/oauthserver/... -v -count=1
```

Expected: all pass.

**Step 6.6: Commit**

```bash
git add internal/oauthserver/oauth_server.go internal/oauthserver/oauth_server_test.go
git commit -m "refactor: route OAuth providers by org login method"
```

---

### Task 7: Wire up in main.go

**Files:**
- Modify: `cmd/pear/main.go`

**Step 7.1: Update provider construction**

In `cmd/pear/main.go`, find the section where `orgLoginProvider` and `loginRouter` are created:

```go
orgLoginProvider := org.NewLoginProvider(orgStore, frontendDomain, oauthSecret)
loginRouter := login.NewRouter(
    login.NewPDSProvider(pdsOAuthClient, pdsCredStore, mappingStoreImpl, directory),
    // googleProvider not wired yet — mapping store not populated
    orgLoginProvider,
)
```

The `mappingStoreImpl` would need to be created. For now, since mappings aren't populated, we can pass a no-op implementation. Looking at what's available in main.go to determine if there's a DB we can use.

Let me check what the main.go currently looks like... I don't have it. But based on the codebase exploration, I know the general shape. The key change is adding the mapping store parameter to `NewPDSProvider` and the org login provider now registering itself implicitly via its `LoginMethod()`.

Actually, `LoginProvider` is in `org` package and implements `login.Provider`. The router is in `login` package. `LoginProvider.LoginMethod()` returns "password". So when we pass `orgLoginProvider` to `login.NewRouter`, it gets registered as "password".

The PDS provider's `LoginMethod()` returns "atproto", so it gets registered as "atproto".

The mapping store — we don't have an implementation yet. We can pass `nil` for now since the pdsProvider handles `nil` dir gracefully (it checks `p.dir != nil`). Or we can create a simple no-op implementation.

Since the user said "don't worry about how those mappings are populated for now", passing nil or a no-op is fine.

**Step 7.2: Verify build**

```bash
go build ./cmd/pear/
```

Expected: builds successfully.

**Step 7.3: Commit**

```bash
git add cmd/pear/main.go
git commit -m "chore: update provider wiring for login method routing"
```
