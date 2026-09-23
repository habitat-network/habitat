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
		resolver:   NewEmailResolver(db, emailStore, h, osStore.Store),
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
