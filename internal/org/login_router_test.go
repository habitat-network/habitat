package org

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/habitat-network/habitat/internal/core"
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
		string(core.LoginMethodGoogle),
		"test@gmail.com",
		"subdomain",
	)
	t.Logf("orgId: %s", orgId.DID.String())
	t.Logf("adminId: %s", adminId.DID.String())

	require.NoError(t, err)
	router := LoginRouter{
		Google:   &dummyProvider{loginID: "test@gmail.com"},
		OrgStore: store,
	}

	t.Run("member login", func(t *testing.T) {
		redirectUri, state, err := router.Authorize(t.Context(), adminId.DID)
		require.NoError(t, err)
		require.Contains(t, redirectUri, "test%40gmail.com")

		err = router.Exchange(
			t.Context(),
			adminId.DID,
			"",
			"",
			state,
		)
		require.NoError(t, err)
	})

	t.Run("org login", func(t *testing.T) {
		redirectUri, state, err := router.Authorize(t.Context(), orgId.DID)
		require.NoError(t, err)
		require.Equal(t, redirectUri, "https://redirect.com?login_hint=")

		err = router.Exchange(
			t.Context(),
			orgId.DID,
			"",
			"",
			state,
		)
		require.NoError(t, err)
	})
}

func TestExchange_RequireAdminForOrg(t *testing.T) {
	store := newTestStore(t)
	orgId, adminId, err := store.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"",
		string(core.LoginMethodGoogle),
		"test@gmail.com",
		"subdomain",
	)
	require.NoError(t, err)
	o, err := store.GetOrg(t.Context(), orgId.DID)
	require.NoError(t, err)
	token, err := o.IssueIdentityToken(t.Context(), adminId.DID, true, time.Now().Add(time.Hour))
	require.NoError(t, err)
	memberId, err := o.CreateNewMemberIdentity(t.Context(), token, "alice", "", "")
	require.NoError(t, err)

	router := LoginRouter{
		Google:   &dummyProvider{loginID: memberId.DID.String()},
		OrgStore: store,
	}
	err = router.Exchange(
		t.Context(),
		orgId.DID,
		"",
		"",
		nil,
	)
	require.Error(t, err)
}

func TestExchange_MismatchLoginID(t *testing.T) {
	store := newTestStore(t)
	_, adminId, err := store.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"",
		string(core.LoginMethodGoogle),
		"test@gmail.com",
		"subdomain",
	)
	require.NoError(t, err)
	router := LoginRouter{
		Google:   &dummyProvider{loginID: "differentId@gmail.com"},
		OrgStore: store,
	}

	err = router.Exchange(
		t.Context(),
		adminId.DID,
		"",
		"",
		nil,
	)
	require.Error(t, err)
}

func TestExchange_MissingMember(t *testing.T) {
	store := newTestStore(t)
	_, adminId, err := store.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"",
		string(core.LoginMethodGoogle),
		"test@gmail.com",
		"subdomain",
	)
	require.NoError(t, err)
	router := LoginRouter{
		Google:   &dummyProvider{loginID: "differentId@gmail.com"},
		OrgStore: store,
	}

	err = router.Exchange(t.Context(), adminId.DID, "", "", nil)
	require.Error(t, err)
}
