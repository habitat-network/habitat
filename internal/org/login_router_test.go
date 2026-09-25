package org_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/emaildomain"
	login_testutil "github.com/habitat-network/habitat/internal/login/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/org/testutil"
)

func TestLoginRouter(t *testing.T) {
	store := testutil.NewTestStore(t)
	orgID, adminID, err := store.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"",
		string(org.LoginMethodGoogle),
		"test@gmail.com",
		"subdomain",
		"contact@example.com",
	)
	t.Logf("orgID: %s", orgID.DID.String())
	t.Logf("adminID: %s", adminID.DID.String())

	require.NoError(t, err)
	router := org.LoginRouter{
		Google:   login_testutil.NewPassthroughProvider(t),
		OrgStore: store,
	}

	t.Run("member login", func(t *testing.T) {
		_, state, err := router.Authorize(t.Context(), adminID.DID)
		require.NoError(t, err)

		err = router.Exchange(
			t.Context(),
			adminID.DID,
			url.Values{},
			state,
		)
		require.NoError(t, err)
	})

	t.Run("org login", func(t *testing.T) {
		p := login_testutil.NewPassthroughProvider(t)
		p.LoginID = "test@gmail.com"
		router := org.LoginRouter{
			Google:   p,
			OrgStore: store,
		}
		_, state, err := router.Authorize(t.Context(), orgID.DID)
		require.NoError(t, err)

		err = router.Exchange(
			t.Context(),
			orgID.DID,
			url.Values{},
			state,
		)
		require.NoError(t, err)
	})
}

func TestExchange_RequireAdminForOrg(t *testing.T) {
	store := testutil.NewTestStore(t)
	orgID, _, err := store.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"",
		string(org.LoginMethodGoogle),
		"test@gmail.com",
		"subdomain",
		"contact@example.com",
	)
	require.NoError(t, err)
	router := org.LoginRouter{
		Google:   login_testutil.NewPassthroughProvider(t),
		OrgStore: store,
	}
	err = router.Exchange(
		t.Context(),
		orgID.DID,
		url.Values{},
		nil,
	)
	require.Error(t, err)
}

func TestExchange_MismatchLoginID(t *testing.T) {
	store := testutil.NewTestStore(t)
	_, adminID, err := store.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"",
		string(org.LoginMethodGoogle),
		"test@gmail.com",
		"subdomain",
		"contact@example.com",
	)
	require.NoError(t, err)
	router := org.LoginRouter{
		Google:   login_testutil.NewPassthroughProvider(t),
		OrgStore: store,
	}

	err = router.Exchange(
		t.Context(),
		adminID.DID,
		url.Values{},
		nil,
	)
	require.Error(t, err)
}

func TestExchange_MissingMember(t *testing.T) {
	store := testutil.NewTestStore(t)
	_, adminID, err := store.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"",
		string(org.LoginMethodGoogle),
		"test@gmail.com",
		"subdomain",
		"contact@example.com",
	)
	require.NoError(t, err)
	router := org.LoginRouter{
		Google:   login_testutil.NewPassthroughProvider(t),
		OrgStore: store,
	}

	err = router.Exchange(t.Context(), adminID.DID, url.Values{}, nil)
	require.Error(t, err)
}

func TestLoginRouterEmailDomain(t *testing.T) {
	db := db_testutil.NewDB(t)
	emailStore, err := emaildomain.NewStore(db)
	require.NoError(t, err)
	osStore := opensocial_testutil.NewTestStore(t, opensocial_testutil.WithDB(db))
	orgDIDStr, err := osStore.NewOrgWithoutCreator(t.Context(), "acme")
	require.NoError(t, err)
	orgDID := syntax.DID(orgDIDStr)
	alice := syntax.DID("did:web:alice.example.com")
	require.NoError(t, emailStore.CreateDomainMapping(
		t.Context(), "acme.com", orgDID, emaildomain.LoginMethodGoogle,
	))
	require.NoError(t, emailStore.Provision(t.Context(), "alice@acme.com", orgDID, alice))
	orgStore := testutil.NewTestStore(t)

	t.Run("google login for provisioned email grants org membership", func(t *testing.T) {
		p := login_testutil.NewPassthroughProvider(t)
		router := org.LoginRouter{
			Google: p, OrgStore: orgStore, EmailStore: emailStore, OpensocialStore: osStore.Store,
		}
		_, state, err := router.Authorize(t.Context(), alice)
		require.NoError(t, err)
		// The provisioned email is passed as Google's login_hint.
		require.Equal(t, "alice@acme.com", p.LoginID)

		// Resolving/authorizing the identity alone must not enroll it.
		roles, err := osStore.GetUserRoles(t.Context(), orgDID, alice)
		require.NoError(t, err)
		require.Empty(t, roles)

		require.NoError(t, router.Exchange(t.Context(), alice, url.Values{}, state))

		roles, err = osStore.GetUserRoles(t.Context(), orgDID, alice)
		require.NoError(t, err)
		require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)
	})

	t.Run("google email differing only in case is accepted", func(t *testing.T) {
		p := login_testutil.NewPassthroughProvider(t)
		p.LoginID = "Alice@Acme.com"
		router := org.LoginRouter{
			Google: p, OrgStore: orgStore, EmailStore: emailStore, OpensocialStore: osStore.Store,
		}
		require.NoError(t, router.Exchange(t.Context(), alice, url.Values{}, nil))
	})

	t.Run("mismatched google email is rejected and grants no membership", func(t *testing.T) {
		bob := syntax.DID("did:web:bob.example.com")
		require.NoError(t, emailStore.Provision(t.Context(), "bob@acme.com", orgDID, bob))
		p := login_testutil.NewPassthroughProvider(t)
		p.LoginID = "mallory@acme.com"
		router := org.LoginRouter{
			Google: p, OrgStore: orgStore, EmailStore: emailStore, OpensocialStore: osStore.Store,
		}
		require.Error(t, router.Exchange(t.Context(), bob, url.Values{}, nil))

		roles, err := osStore.GetUserRoles(t.Context(), orgDID, bob)
		require.NoError(t, err)
		require.Empty(t, roles)
	})

	t.Run("google not configured", func(t *testing.T) {
		router := org.LoginRouter{OrgStore: orgStore, EmailStore: emailStore}
		_, _, err := router.Authorize(t.Context(), alice)
		require.ErrorContains(t, err, "unsupported login provider")
		err = router.Exchange(t.Context(), alice, url.Values{}, nil)
		require.ErrorContains(t, err, "unsupported login provider")
	})
}

// fakeProvisioner provisions a fixed DID per email into emailStore, recording
// every email it was asked to provision.
type fakeProvisioner struct {
	emailStore *emaildomain.Store
	orgDID     syntax.DID
	dids       map[emaildomain.Email]syntax.DID
	calls      []emaildomain.Email
}

func (f *fakeProvisioner) ProvisionEmailIdentity(
	ctx context.Context,
	email emaildomain.Email,
) (syntax.DID, error) {
	f.calls = append(f.calls, email)
	did := f.dids[email]
	if err := f.emailStore.Provision(ctx, email, f.orgDID, did); err != nil {
		return "", err
	}
	return did, nil
}

func TestLoginRouterNewEmail(t *testing.T) {
	db := db_testutil.NewDB(t)
	emailStore, err := emaildomain.NewStore(db)
	require.NoError(t, err)
	osStore := opensocial_testutil.NewTestStore(t, opensocial_testutil.WithDB(db))
	orgDIDStr, err := osStore.NewOrgWithoutCreator(t.Context(), "acme")
	require.NoError(t, err)
	orgDID := syntax.DID(orgDIDStr)
	require.NoError(t, emailStore.CreateDomainMapping(
		t.Context(), "acme.com", orgDID, emaildomain.LoginMethodGoogle,
	))
	alice := syntax.DID("did:web:alice.example.com")
	bob := syntax.DID("did:web:bob.example.com")
	newRouter := func(p *login_testutil.PassthroughProvider) (org.LoginRouter, *fakeProvisioner) {
		prov := &fakeProvisioner{
			emailStore: emailStore,
			orgDID:     orgDID,
			dids: map[emaildomain.Email]syntax.DID{
				"alice@acme.com": alice,
				"bob@acme.com":   bob,
			},
		}
		return org.LoginRouter{
			Google:           p,
			EmailStore:       emailStore,
			OpensocialStore:  osStore.Store,
			EmailProvisioner: prov,
		}, prov
	}

	t.Run("mismatched google email provisions nothing", func(t *testing.T) {
		p := login_testutil.NewPassthroughProvider(t)
		p.LoginID = "mallory@acme.com"
		router, prov := newRouter(p)
		_, err := router.ExchangeEmail(t.Context(), "bob@acme.com", url.Values{}, nil)
		require.ErrorContains(t, err, "login id mismatch")
		require.Empty(t, prov.calls)
		_, ok, err := emailStore.GetDID(t.Context(), "bob@acme.com")
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("verified google email provisions identity and membership", func(t *testing.T) {
		p := login_testutil.NewPassthroughProvider(t)
		router, prov := newRouter(p)
		_, state, err := router.AuthorizeEmail(t.Context(), "alice@acme.com")
		require.NoError(t, err)
		// The email is passed as Google's login_hint, and nothing is minted
		// just by starting sign-in.
		require.Equal(t, "alice@acme.com", p.LoginID)
		require.Empty(t, prov.calls)

		p.LoginID = "Alice@Acme.com"
		did, err := router.ExchangeEmail(t.Context(), "alice@acme.com", url.Values{}, state)
		require.NoError(t, err)
		require.Equal(t, alice, did)
		require.Equal(t, []emaildomain.Email{"alice@acme.com"}, prov.calls)

		roles, err := osStore.GetUserRoles(t.Context(), orgDID, alice)
		require.NoError(t, err)
		require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)
	})

	t.Run("unmapped domain", func(t *testing.T) {
		router, prov := newRouter(login_testutil.NewPassthroughProvider(t))
		_, _, err := router.AuthorizeEmail(t.Context(), "alice@other.com")
		require.ErrorContains(t, err, "not set up for sign-in")
		_, err = router.ExchangeEmail(t.Context(), "alice@other.com", url.Values{}, nil)
		require.ErrorContains(t, err, "not set up for sign-in")
		require.Empty(t, prov.calls)
	})

	t.Run("not configured", func(t *testing.T) {
		router := org.LoginRouter{Google: login_testutil.NewPassthroughProvider(t)}
		_, _, err := router.AuthorizeEmail(t.Context(), "alice@acme.com")
		require.ErrorContains(t, err, "not configured")
		_, err = router.ExchangeEmail(t.Context(), "alice@acme.com", url.Values{}, nil)
		require.ErrorContains(t, err, "not configured")

		router = org.LoginRouter{EmailStore: emailStore, EmailProvisioner: &fakeProvisioner{}}
		_, _, err = router.AuthorizeEmail(t.Context(), "alice@acme.com")
		require.ErrorContains(t, err, "unsupported login provider")
	})
}
