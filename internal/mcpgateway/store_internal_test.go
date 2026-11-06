package mcpgateway

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/keyval"
	keyval_testutil "github.com/habitat-network/habitat/internal/keyval/testutil"
	"github.com/habitat-network/habitat/internal/mcpgateway/mcpgatewaytest"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type noOrgs struct{}

func (noOrgs) ListMemberSpaces(
	context.Context,
	syntax.DID,
) ([]habitat_syntax.SpaceURI, error) {
	return nil, nil
}

// newConnectedStore returns a store with the OAuth server "linear" backed by
// fake, which did has connected to.
func newConnectedStore(
	t *testing.T,
	fake *mcpgatewaytest.Server,
	did syntax.DID,
) (*store, *serverRow) {
	t.Helper()
	kv := keyval_testutil.New(t)
	encKV, err := keyval.NewEncrypted(kv, encrypt.TestKey)
	require.NoError(t, err)
	const callback = "https://pear.example.com/mcp-oauth/callback"
	s, err := NewStore(
		db_testutil.NewDB(t, Models()), encKV, kv, encrypt.TestKey, noOrgs{},
		http.DefaultClient, callback,
	)
	require.NoError(t, err)
	st := s.(*store)
	org := syntax.DID("did:web:org.example.com")
	_, err = st.AddServer(t.Context(), org, NewServer{
		Name: "linear", URL: fake.URL, AuthType: AuthTypeOAuth,
	})
	require.NoError(t, err)
	authURL, err := st.StartAuthorization(t.Context(), did, org, "linear", "")
	require.NoError(t, err)
	q := fake.Authorize(t, authURL)
	_, err = st.CompleteAuthorization(t.Context(), q.Get("state"), q.Get("code"), q.Get("iss"))
	require.NoError(t, err)
	row, err := st.getRow(t.Context(), org, "linear")
	require.NoError(t, err)
	return st, row
}

// A caller that read the tokens before another caller refreshed them must not
// redeem the refresh token it read: the authorization server rotated it, so
// redeeming it again would be refused and disconnect the member. This holds
// even when the refreshed tokens have no expiry.
func TestRefreshUsesCurrentRefreshToken(t *testing.T) {
	fake := mcpgatewaytest.New(t,
		mcpgatewaytest.WithAccessTokenTTL(30*time.Second),
		mcpgatewaytest.WithRefreshedTokenTTL(0),
	)
	did := syntax.DID("did:plc:alice")
	s, row := newConnectedStore(t, fake, did)

	stale, err := s.token(t.Context(), row, did)
	require.NoError(t, err)
	require.True(t, stale.needsRefresh())

	// Another caller refreshes first.
	_, err = s.Downstream(t.Context(), did, row.OrgDID, row.Name)
	require.NoError(t, err)
	require.Equal(t, 1, fake.RefreshCount())

	// The caller that read the stale tokens gets the refreshed ones, without
	// another refresh.
	got, err := s.refresh(t.Context(), row, did)
	require.NoError(t, err)
	require.NotEqual(t, stale.AccessToken, got.AccessToken)
	require.True(t, got.Expiry.IsZero())
	require.Equal(t, 1, fake.RefreshCount())
	_, err = s.Downstream(t.Context(), did, row.OrgDID, row.Name)
	require.NoError(t, err)
}

// A refresh isn't cut short when the request that started it is cancelled:
// once the authorization server has rotated the refresh token, the new one
// must be stored or the member is disconnected at the next refresh.
func TestRefreshOutlivesCancelledCaller(t *testing.T) {
	fake := mcpgatewaytest.New(t, mcpgatewaytest.WithAccessTokenTTL(30*time.Second))
	did := syntax.DID("did:plc:alice")
	s, row := newConnectedStore(t, fake, did)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := s.refresh(ctx, row, did)
	require.NoError(t, err)
	require.Equal(t, 1, fake.RefreshCount())

	// The rotated refresh token was stored, so the next refresh works.
	_, err = s.Downstream(t.Context(), did, row.OrgDID, row.Name)
	require.NoError(t, err)
	require.Equal(t, 2, fake.RefreshCount())
}

func TestStoredTokenNeedsRefresh(t *testing.T) {
	require.False(t, (&storedToken{}).needsRefresh(), "no expiry")
	require.False(t, (&storedToken{Expiry: time.Now().Add(time.Hour)}).needsRefresh())
	require.True(t, (&storedToken{Expiry: time.Now().Add(time.Second)}).needsRefresh())
	require.True(t, (&storedToken{Expiry: time.Now().Add(-time.Hour)}).needsRefresh())
}
