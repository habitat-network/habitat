package identity

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
		resolver:   NewEmailResolver(db, emailStore, h),
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

// Resolving an email never mints: an email at a mapped domain that hasn't
// signed in reads as not provisioned, and leaves no trace behind — so
// mistyped, abandoned, or bot-entered emails can't create identities.
func TestEmailResolverResolveDoesNotMint(t *testing.T) {
	f := newEmailFixture(t)
	_, err := f.resolver.ResolveEmailIdentity(t.Context(), "alice@acme.com")
	require.ErrorIs(t, err, emaildomain.ErrEmailNotProvisioned)
	require.ErrorIs(t, err, identity.ErrDIDNotFound)

	_, ok, err := f.emailStore.GetDID(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.False(t, ok)
	_, err = f.hive.LookupHandle(t.Context(), "alice.acme.example.com")
	require.Error(t, err)
}

func TestEmailResolverResolvesProvisioned(t *testing.T) {
	f := newEmailFixture(t)
	did, err := f.resolver.ProvisionEmailIdentity(t.Context(), "alice@acme.com")
	require.NoError(t, err)

	// Differently-cased input normalizes to the same email.
	email, err := emaildomain.ParseEmail("Alice@ACME.com")
	require.NoError(t, err)
	ident, err := f.resolver.ResolveEmailIdentity(t.Context(), email)
	require.NoError(t, err)
	require.Equal(t, did, ident.DID)
	require.Equal(t, syntax.Handle("alice.acme.example.com"), ident.Handle)
}

// Provisioning an identity must not, by itself, enroll it in the org: that's
// left to the sign-in flow (see org.LoginRouter).
func TestEmailResolverProvisionsWithoutOrgMembership(t *testing.T) {
	f := newEmailFixture(t)
	did, err := f.resolver.ProvisionEmailIdentity(t.Context(), "alice@acme.com")
	require.NoError(t, err)

	roles, err := f.opensocial.GetUserRoles(t.Context(), f.org, did)
	require.NoError(t, err)
	require.Empty(t, roles)
	require.Equal(t, 0, f.memberships(t))

	got, ok, err := f.emailStore.GetDID(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, did, got)

	served, err := f.hive.LookupDID(t.Context(), did)
	require.NoError(t, err)
	require.Equal(t, did, served.DID)
	require.Equal(t, syntax.Handle("alice.acme.example.com"), served.Handle)
}

func TestEmailResolverProvisionDistinctEmails(t *testing.T) {
	f := newEmailFixture(t)
	alice, err := f.resolver.ProvisionEmailIdentity(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	bob, err := f.resolver.ProvisionEmailIdentity(t.Context(), "bob@acme.com")
	require.NoError(t, err)
	require.NotEqual(t, alice, bob)
}

func TestEmailResolverProvisionReturningMember(t *testing.T) {
	f := newEmailFixture(t)
	first, err := f.resolver.ProvisionEmailIdentity(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	again, err := f.resolver.ProvisionEmailIdentity(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.Equal(t, first, again)
}

func TestEmailResolverUnknownDomain(t *testing.T) {
	f := newEmailFixture(t)
	_, err := f.resolver.ResolveEmailIdentity(t.Context(), "alice@other.com")
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
	require.NotErrorIs(t, err, emaildomain.ErrEmailNotProvisioned)

	_, err = f.resolver.ProvisionEmailIdentity(t.Context(), "alice@other.com")
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
	_, ok, err := f.emailStore.GetDID(t.Context(), "alice@other.com")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestEmailResolverHandleCollision(t *testing.T) {
	f := newEmailFixture(t)
	alice, err := f.resolver.ProvisionEmailIdentity(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	// "a.lice" sanitizes to the same "alice" handle prefix.
	otherDID, err := f.resolver.ProvisionEmailIdentity(t.Context(), "a.lice@acme.com")
	require.NoError(t, err)
	require.NotEqual(t, alice, otherDID)
	other, err := f.hive.LookupDID(t.Context(), otherDID)
	require.NoError(t, err)
	require.Regexp(t, `^alice[0-9a-f]{8}\.acme\.example\.com$`, other.Handle.String())
}

func TestEmailResolverConcurrentSameEmail(t *testing.T) {
	f := newEmailFixture(t)
	var (
		wg      sync.WaitGroup
		results [2]syntax.DID
		errs    [2]error
	)
	for i := range 2 {
		wg.Go(func() {
			results[i], errs[i] = f.resolver.ProvisionEmailIdentity(t.Context(), "alice@acme.com")
		})
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, results[0], results[1])
	// The losing attempt rolled back its email->DID mapping write, leaving a
	// single mapping and no org membership (provisioning never grants one).
	did, ok, err := f.emailStore.GetDID(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, results[0], did)
	require.Equal(t, 0, f.memberships(t))
}

// noNetworkTransport fails every request, so SpaceProxyDirectory.applyOverride's
// spaces probe of a minted identity's PDS reads as "unsupported" without real
// network.
type noNetworkTransport struct{}

func (noNetworkTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("network disabled in tests")
}

func emailServer(f emailFixture, opts ...func(*Server)) *Server {
	s := &Server{
		hive: f.hive,
		directory: NewSpaceProxyDirectory(
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

func TestResolveIdentityEmail(t *testing.T) {
	f := newEmailFixture(t)
	did, err := f.resolver.ProvisionEmailIdentity(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	var out atproto.IdentityDefs_IdentityInfo
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		emailServer(f).ResolveIdentity,
		url.Values{"identifier": []string{"alice@acme.com"}},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, did.String(), out.Did)
	require.Equal(t, "alice.acme.example.com", out.Handle)
}

// The public resolveIdentity endpoint is reachable before any sign-in, so it
// must not mint an identity for an email that hasn't signed in; it tells the
// caller so with a distinct error instead.
func TestResolveIdentityEmailNotProvisioned(t *testing.T) {
	f := newEmailFixture(t)
	var out struct {
		Error string `json:"error"`
	}
	req := httptest.NewRequest(
		http.MethodGet, "/?"+url.Values{"identifier": {"alice@acme.com"}}.Encode(), http.NoBody,
	)
	w := httptest.NewRecorder()
	emailServer(f).ResolveIdentity(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.Equal(t, "EmailNotProvisioned", out.Error)

	_, ok, err := f.emailStore.GetDID(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestResolveHandleEmail(t *testing.T) {
	f := newEmailFixture(t)
	did, err := f.resolver.ProvisionEmailIdentity(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	var out atproto.IdentityResolveHandle_Output
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		emailServer(f).ResolveHandle,
		url.Values{"handle": []string{"alice@acme.com"}},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, did.String(), out.Did)
}

func TestResolveHandleEmailNotProvisioned(t *testing.T) {
	f := newEmailFixture(t)
	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		emailServer(f).ResolveHandle,
		url.Values{"handle": []string{"alice@acme.com"}},
		&out,
	)
	require.Equal(t, http.StatusNotFound, code)
	_, ok, err := f.emailStore.GetDID(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.False(t, ok)
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
