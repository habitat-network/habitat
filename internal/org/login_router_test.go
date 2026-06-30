package org

import (
	"context"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
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

	router := LoginRouter{
		Google:   &dummyProvider{loginID: "test@gmail.com"},
		OrgStore: store,
	}

	t.Run("member login uses the member's login hint", func(t *testing.T) {
		member, err := store.GetMember(t.Context(), adminId.DID)
		require.NoError(t, err)

		redirectUri, _, err := router.Authorize(t.Context(), member.DID)
		require.NoError(t, err)
		require.Contains(t, redirectUri, "test%40gmail.com")

		exchanged, err := router.Exchange(t.Context(), member.DID, "", "", nil)
		require.NoError(t, err)
		require.Equal(t, member.DID, exchanged.DID)
	})

	t.Run("org login uses an empty login hint", func(t *testing.T) {
		redirectUri, _, err := router.Authorize(t.Context(), orgId.DID)
		require.NoError(t, err)
		require.Equal(t, "https://redirect.com?login_hint=", redirectUri)

		exchanged, err := router.Exchange(t.Context(), orgId.DID, "", "", nil)
		require.NoError(t, err)
		require.Equal(t, adminId.DID, exchanged.DID)
	})

	t.Run("unsupported login provider returns an error", func(t *testing.T) {
		emptyRouter := LoginRouter{OrgStore: store}

		_, _, err := emptyRouter.Authorize(t.Context(), orgId.DID)
		require.Error(t, err)

		_, err = emptyRouter.Exchange(t.Context(), orgId.DID, "", "", nil)
		require.Error(t, err)
	})

	t.Run("org login requires the exchanged login id to belong to an admin", func(t *testing.T) {
		nonAdminRouter := LoginRouter{
			Google:   &dummyProvider{loginID: "not-a-member@gmail.com"},
			OrgStore: store,
		}

		_, err := nonAdminRouter.Exchange(t.Context(), orgId.DID, "", "", nil)
		require.Error(t, err)
	})

	t.Run("member login requires the exchanged login id to match the member", func(t *testing.T) {
		mismatchRouter := LoginRouter{
			Google:   &dummyProvider{loginID: "someone-else@gmail.com"},
			OrgStore: store,
		}

		member, err := store.GetMember(t.Context(), adminId.DID)
		require.NoError(t, err)

		_, err = mismatchRouter.Exchange(t.Context(), member.DID, "", "", nil)
		require.Error(t, err)
	})

	t.Run("returns an error when no org or member is found for the did", func(t *testing.T) {
		unknownDid := syntax.DID("did:web:unknown.example.com")

		_, _, err := router.Authorize(t.Context(), unknownDid)
		require.Error(t, err)

		_, err = router.Exchange(t.Context(), unknownDid, "", "", nil)
		require.Error(t, err)
	})
}
