package org

import (
	"context"
	"net/url"
	"testing"

	"github.com/habitat-network/habitat/internal/login"
	"github.com/stretchr/testify/require"
)

type dummyProvider struct{ loginID string }

var _ login.Provider = (*dummyProvider)(nil)

// Authorize implements [login.Provider].
func (d *dummyProvider) Authorize(
	ctx context.Context,
	loginHint string,
) (redirectURI string, state []byte, err error) {
	return "https://redirect.com?login_hint=" + url.QueryEscape(loginHint), nil, nil
}

// Exchange implements [login.Provider].
func (d *dummyProvider) Exchange(
	ctx context.Context,
	code string,
	issuer string,
	state []byte,
) (loginId string, err error) {
	return d.loginID, nil
}

func TestLoginRouter(t *testing.T) {
	store := newTestStore(t)
	orgId, adminId, err := store.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"",
		string(LoginMethodGoogle),
		"test@gmail.com",
		"subdomain",
	)
	require.NoError(t, err)

	fetchedOrg, err := store.GetOrg(t.Context(), orgId.DID)
	require.NoError(t, err)

	router := LoginRouter{Google: &dummyProvider{loginID: "test@gmail.com"}}

	t.Run("member login uses the member's login hint", func(t *testing.T) {
		member, err := store.GetMember(t.Context(), adminId.DID)
		require.NoError(t, err)

		redirectUri, _, err := router.Authorize(t.Context(), member.Org, member.LoginID)
		require.NoError(t, err)
		require.Contains(t, redirectUri, "test%40gmail.com")

		loginID, err := router.Exchange(t.Context(), member.Org, "", "", nil)
		require.NoError(t, err)
		require.Equal(t, "test@gmail.com", loginID)
	})

	t.Run("org login uses an empty login hint", func(t *testing.T) {
		redirectUri, _, err := router.Authorize(t.Context(), fetchedOrg, "")
		require.NoError(t, err)
		require.Equal(t, "https://redirect.com?login_hint=", redirectUri)

		loginID, err := router.Exchange(t.Context(), fetchedOrg, "", "", nil)
		require.NoError(t, err)
		require.Equal(t, "test@gmail.com", loginID)
	})

	t.Run("unsupported login provider returns an error", func(t *testing.T) {
		emptyRouter := LoginRouter{}

		_, _, err := emptyRouter.Authorize(t.Context(), fetchedOrg, "")
		require.Error(t, err)

		_, err = emptyRouter.Exchange(t.Context(), fetchedOrg, "", "", nil)
		require.Error(t, err)
	})
}
