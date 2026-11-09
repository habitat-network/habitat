package mcpgateway

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

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
		db_testutil.NewDB(t, Models()), encKV, encrypt.TestKey, noOrgs{},
		http.DefaultClient, callback,
	)
	require.NoError(t, err)
	st := s.(*store)
	org := syntax.DID("did:web:org.example.com")
	_, err = st.AddServer(t.Context(), org, NewServer{
		Name: "linear", URL: fake.URL, AuthType: AuthTypeOAuth,
	})
	require.NoError(t, err)
	authURL, err := st.StartAuthorization(t.Context(), did, org, "linear")
	require.NoError(t, err)
	q := fake.Authorize(t, authURL)
	require.NoError(
		t, st.CompleteAuthorization(t.Context(), q.Get("state"), q.Get("code"), q.Get("iss")),
	)
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

	stale, err := s.vault.get(t.Context(), row.grant(did))
	require.NoError(t, err)
	require.True(t, stale.needsRefresh())

	// Another caller refreshes first.
	_, err = s.Downstream(t.Context(), did, row.OrgDID, row.Name)
	require.NoError(t, err)
	require.Equal(t, 1, fake.RefreshCount())

	// The caller that read the stale tokens gets the refreshed ones, without
	// another refresh.
	cfg, err := s.oauthConfig(row)
	require.NoError(t, err)
	got, err := s.vault.refresh(t.Context(), row.grant(did), cfg)
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
	cfg, err := s.oauthConfig(row)
	require.NoError(t, err)
	_, err = s.vault.refresh(ctx, row.grant(did), cfg)
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

// An expired access token without a refresh token can't be renewed, so the
// member has to connect again; their tokens are left for them to replace.
func TestRefreshWithoutRefreshToken(t *testing.T) {
	fake := mcpgatewaytest.New(t)
	did := syntax.DID("did:plc:alice")
	s, row := newConnectedStore(t, fake, did)
	require.NoError(t, s.vault.put(t.Context(), row.grant(did), &oauth2.Token{
		AccessToken: "expired", TokenType: "Bearer", Expiry: time.Now().Add(-time.Hour),
	}))

	_, err := s.Downstream(t.Context(), did, row.OrgDID, row.Name)
	require.ErrorIs(t, err, ErrNotConnected)
	require.Zero(t, fake.RefreshCount())
}

// Whatever case the authorization server gives the token type in, the
// downstream request carries a well-formed bearer Authorization header.
func TestDownstreamTokenTypes(t *testing.T) {
	fake := mcpgatewaytest.New(t)
	did := syntax.DID("did:plc:alice")
	s, row := newConnectedStore(t, fake, did)

	for tokenType, want := range map[string]string{
		"":       "Bearer x",
		"bearer": "Bearer x",
		"Bearer": "Bearer x",
	} {
		require.NoError(t, s.vault.put(t.Context(), row.grant(did), &oauth2.Token{
			AccessToken: "x", TokenType: tokenType,
		}))
		d, err := s.Downstream(t.Context(), did, row.OrgDID, row.Name)
		require.NoError(t, err)
		require.Equal(t, want, d.Headers["Authorization"], tokenType)
	}
}

// An OAuth server takes no token: members authorize with it instead.
func TestConnectWithTokenOAuthServer(t *testing.T) {
	did := syntax.DID("did:plc:alice")
	s, row := newConnectedStore(t, mcpgatewaytest.New(t), did)
	require.ErrorIs(
		t,
		s.ConnectWithToken(t.Context(), did, row.OrgDID, row.Name, "token"),
		ErrTokenNotSupported,
	)
}

// The OAuth configuration is embedded in the row under the columns it has
// always had, so embedding it needs no migration.
func TestServerRowColumns(t *testing.T) {
	stmt := &gorm.Statement{DB: db_testutil.NewDB(t, Models())}
	require.NoError(t, stmt.Parse(&serverRow{}))
	require.ElementsMatch(t, []string{
		"org_did", "name", "description", "url", "auth_type", "generation",
		"oauth_resource", "oauth_issuer", "oauth_auth_url", "oauth_token_url",
		"oauth_iss_parameter_supported", "oauth_scopes", "oauth_client_id",
		"oauth_auth_style", "oauth_client_secret_enc", "created_at", "updated_at",
	}, stmt.Schema.DBNames)
	require.Equal(t, schema.String, stmt.Schema.FieldsByDBName["oauth_scopes"].DataType)
}

// A server's OAuth configuration, scopes and client secret included, reads
// back from its row as it was discovered.
func TestOAuthConfigRoundTrip(t *testing.T) {
	did := syntax.DID("did:plc:alice")
	s, _ := newConnectedStore(t, mcpgatewaytest.New(t), did)
	org := syntax.DID("did:web:org.example.com")
	fake := mcpgatewaytest.New(t, mcpgatewaytest.WithoutDCR())
	_, err := s.AddServer(t.Context(), org, NewServer{
		Name: "prereg", URL: fake.URL, AuthType: AuthTypeOAuth,
		ClientID:     fake.RegisterClient("https://pear.example.com/mcp-oauth/callback"),
		ClientSecret: "shh",
	})
	require.NoError(t, err)

	row, err := s.getRow(t.Context(), org, "prereg")
	require.NoError(t, err)
	require.Empty(t, row.OAuth.ClientSecret, "the secret is only stored encrypted")
	cfg, err := s.oauthConfig(row)
	require.NoError(t, err)
	require.Equal(t, "shh", cfg.ClientSecret)
	require.Equal(t, fake.Issuer, cfg.Issuer)
	require.NotEmpty(t, cfg.TokenURL)
	require.NotEmpty(t, cfg.Resource)
}

func TestScopes(t *testing.T) {
	v, err := scopes{"read", "offline_access"}.Value()
	require.NoError(t, err)
	require.Equal(t, "read offline_access", v)

	var s scopes
	require.NoError(t, s.Scan("read offline_access"))
	require.Equal(t, scopes{"read", "offline_access"}, s)
	require.NoError(t, s.Scan([]byte("read")))
	require.Equal(t, scopes{"read"}, s)
	require.NoError(t, s.Scan(nil))
	require.Empty(t, s)
	require.Error(t, s.Scan(42))
}
