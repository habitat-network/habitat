package org_test

import (
	"net/url"
	"testing"

	login_testutil "github.com/habitat-network/habitat/internal/login/testutil"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/org/testutil"
	"github.com/stretchr/testify/require"
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
