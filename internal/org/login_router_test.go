package org_test

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/spaces"
)

// stubProvider is a login.Provider whose results and errors a test controls.
type stubProvider struct {
	loginID      string
	profile      login.Profile
	authorizeErr error
	exchangeErr  error
	loginHint    string
	exchanges    int
}

func (p *stubProvider) Authorize(
	_ context.Context,
	loginHint string,
) (string, string, string, error) {
	p.loginHint = loginHint
	if p.authorizeErr != nil {
		return "", "", "", p.authorizeErr
	}
	return "https://provider.example.com/authorize", "state", "provider-state", nil
}

func (p *stubProvider) Exchange(
	_ context.Context,
	_ url.Values,
	_ string,
) (string, login.Profile, error) {
	p.exchanges++
	if p.exchangeErr != nil {
		return "", login.Profile{}, p.exchangeErr
	}
	return p.loginID, p.profile, nil
}

func newEmailOrg(t *testing.T) (
	*emaildomain.Store,
	*opensocial_testutil.TestStore,
	syntax.DID,
) {
	t.Helper()
	db := db_testutil.NewDB(
		t,
		opensocial.Models(),
		spaces.Models(),
		hive.Models(),
		emaildomain.Models(),
	)
	osStore := opensocial_testutil.NewTestStore(t, opensocial_testutil.WithDB(db))
	emailStore, err := emaildomain.NewStore(db)
	require.NoError(t, err)
	orgDIDStr, err := osStore.NewOrgWithoutCreator(t.Context(), "acme")
	require.NoError(t, err)
	orgDID := syntax.DID(orgDIDStr)
	require.NoError(t, emailStore.CreateDomainMapping(
		t.Context(), "acme.com", orgDID, emaildomain.LoginMethodGoogle,
	))
	return emailStore, osStore, orgDID
}

func TestLoginRouterEmailDomain(t *testing.T) {
	emailStore, osStore, orgDID := newEmailOrg(t)
	alice := syntax.DID("did:web:alice.example.com")
	require.NoError(t, emailStore.Provision(t.Context(), "alice@acme.com", orgDID, alice))

	google := &stubProvider{
		loginID: "alice@acme.com",
		profile: login.Profile{Name: "Alice", Picture: "https://example.com/alice.png"},
	}
	router := &org.LoginRouter{
		Pds:             &stubProvider{},
		Google:          google,
		EmailStore:      emailStore,
		OpensocialStore: osStore.Store,
	}

	t.Run("authorizes with the provisioned email as login hint", func(t *testing.T) {
		redirect, state, key, err := router.Authorize(t.Context(), alice)
		require.NoError(t, err)
		require.NotEmpty(t, redirect)
		require.Equal(t, "state", state)
		require.Equal(t, "provider-state", key)
		require.Equal(t, "alice@acme.com", google.loginHint)

		// Authorizing alone must not enroll the DID.
		roles, err := osStore.GetUserRoles(t.Context(), orgDID, alice)
		require.NoError(t, err)
		require.Empty(t, roles)
	})

	t.Run("exchange grants membership and seeds the profile", func(t *testing.T) {
		require.NoError(t, router.Exchange(t.Context(), alice, url.Values{}, "provider-state"))
		roles, err := osStore.GetUserRoles(t.Context(), orgDID, alice)
		require.NoError(t, err)
		require.ElementsMatch(
			t, []string{opensocial.MemberRoleRkey, opensocial.AdminRoleRkey}, roles,
		)
	})

	t.Run("email match is case-insensitive", func(t *testing.T) {
		google.loginID = "Alice@Acme.com"
		require.NoError(t, router.Exchange(t.Context(), alice, url.Values{}, "provider-state"))
		require.Equal(t, 2, google.exchanges)
	})

	t.Run("mismatched email is rejected and grants no membership", func(t *testing.T) {
		bob := syntax.DID("did:web:bob.example.com")
		require.NoError(t, emailStore.Provision(t.Context(), "bob@acme.com", orgDID, bob))

		mallory := &stubProvider{loginID: "mallory@acme.com"}
		mismatchRouter := &org.LoginRouter{
			Google: mallory, EmailStore: emailStore, OpensocialStore: osStore.Store,
		}
		err := mismatchRouter.Exchange(t.Context(), bob, url.Values{}, "")
		require.ErrorContains(t, err, "login id mismatch")

		roles, err := osStore.GetUserRoles(t.Context(), orgDID, bob)
		require.NoError(t, err)
		require.Empty(t, roles)
	})

	t.Run("provider exchange error is wrapped", func(t *testing.T) {
		carol := syntax.DID("did:web:carol.example.com")
		require.NoError(t, emailStore.Provision(t.Context(), "carol@acme.com", orgDID, carol))

		failing := &stubProvider{exchangeErr: errors.New("boom")}
		failingRouter := &org.LoginRouter{
			Google: failing, EmailStore: emailStore, OpensocialStore: osStore.Store,
		}
		err := failingRouter.Exchange(t.Context(), carol, url.Values{}, "")
		require.ErrorContains(t, err, "failed to exchange code")
	})

	t.Run("unsupported login method is rejected", func(t *testing.T) {
		otherOrgDIDStr, err := osStore.NewOrgWithoutCreator(t.Context(), "legacy")
		require.NoError(t, err)
		otherOrgDID := syntax.DID(otherOrgDIDStr)
		require.NoError(t, emailStore.CreateDomainMapping(
			t.Context(), "legacy.example.com", otherOrgDID, emaildomain.LoginMethod("password"),
		))
		dave := syntax.DID("did:web:dave.example.com")
		require.NoError(
			t, emailStore.Provision(t.Context(), "dave@legacy.example.com", otherOrgDID, dave),
		)

		router := &org.LoginRouter{Google: google, EmailStore: emailStore}
		_, _, _, err = router.Authorize(t.Context(), dave)
		require.ErrorContains(t, err, "unsupported login provider")
		err = router.Exchange(t.Context(), dave, url.Values{}, "")
		require.ErrorContains(t, err, "unsupported login provider")
	})

	t.Run("google not configured is rejected", func(t *testing.T) {
		router := &org.LoginRouter{EmailStore: emailStore}
		_, _, _, err := router.Authorize(t.Context(), alice)
		require.ErrorContains(t, err, "unsupported login provider")
		err = router.Exchange(t.Context(), alice, url.Values{}, "")
		require.ErrorContains(t, err, "unsupported login provider")
	})
}

func TestLoginRouterEmailDomainWithoutOpensocialStore(t *testing.T) {
	emailStore, _, orgDID := newEmailOrg(t)
	alice := syntax.DID("did:web:alice.example.com")
	require.NoError(t, emailStore.Provision(t.Context(), "alice@acme.com", orgDID, alice))

	google := &stubProvider{loginID: "alice@acme.com"}
	router := &org.LoginRouter{Google: google, EmailStore: emailStore}

	_, _, _, err := router.Authorize(t.Context(), alice)
	require.NoError(t, err)
	// Without an opensocial store the member is not enrolled, but sign-in
	// still succeeds.
	require.NoError(t, router.Exchange(t.Context(), alice, url.Values{}, ""))
}

func TestLoginRouterEmailStoreError(t *testing.T) {
	// A database without the email tables makes GetLoginMethod fail.
	brokenStore, err := emaildomain.NewStore(db_testutil.NewDB(t))
	require.NoError(t, err)

	router := &org.LoginRouter{
		Pds:        &stubProvider{},
		Google:     &stubProvider{},
		EmailStore: brokenStore,
	}
	_, _, _, err = router.Authorize(t.Context(), syntax.DID("did:web:alice.example.com"))
	require.ErrorContains(t, err, "get email login method")
	err = router.Exchange(t.Context(), syntax.DID("did:web:alice.example.com"), url.Values{}, "")
	require.ErrorContains(t, err, "get email login method")
}

func TestLoginRouterPDSFallback(t *testing.T) {
	emailStore, _, _ := newEmailOrg(t)
	unknown := syntax.DID("did:web:unknown.example.com")

	t.Run("authorize and exchange fall back to the PDS", func(t *testing.T) {
		pds := &stubProvider{loginID: unknown.String()}
		router := &org.LoginRouter{Pds: pds, Google: &stubProvider{}, EmailStore: emailStore}

		_, _, key, err := router.Authorize(t.Context(), unknown)
		require.NoError(t, err)
		// The PDS is asked to sign in any account, not a specific one.
		require.Empty(t, pds.loginHint)

		require.NoError(t, router.Exchange(t.Context(), unknown, url.Values{}, key))
		require.Equal(t, 1, pds.exchanges)
	})

	t.Run("without an email store", func(t *testing.T) {
		pds := &stubProvider{loginID: unknown.String()}
		router := &org.LoginRouter{Pds: pds}

		_, _, key, err := router.Authorize(t.Context(), unknown)
		require.NoError(t, err)
		require.Empty(t, pds.loginHint)
		require.NoError(t, router.Exchange(t.Context(), unknown, url.Values{}, key))
	})

	t.Run("pds exchange failure is wrapped", func(t *testing.T) {
		pds := &stubProvider{exchangeErr: errors.New("boom")}
		router := &org.LoginRouter{Pds: pds}
		err := router.Exchange(t.Context(), unknown, url.Values{}, "")
		require.ErrorContains(t, err, "failed to exchange code")
	})
}
