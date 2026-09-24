package org_test

import (
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
