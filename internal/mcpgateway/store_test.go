package mcpgateway_test

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
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/mcpgateway/mcpgatewaytest"
	"github.com/habitat-network/habitat/internal/opensocial"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const callbackURL = "https://pear.example.com/mcp-oauth/callback"

var (
	org    = syntax.DID("did:web:org.example.com")
	alice  = syntax.DID("did:plc:alice")
	bob    = syntax.DID("did:plc:bob")
	nobody = syntax.DID("did:plc:nobody")
)

// fakeOrgs makes every user but nobody a member of org.
type fakeOrgs struct{}

func (fakeOrgs) ListMemberSpaces(
	_ context.Context,
	user syntax.DID,
) ([]habitat_syntax.SpaceURI, error) {
	if user == nobody {
		return nil, nil
	}
	return []habitat_syntax.SpaceURI{
		habitat_syntax.ConstructSpaceURI(org, opensocial.MembersSpaceType, "self"),
	}, nil
}

type testStore struct {
	mcpgateway.Store
	kv keyval.Store
}

func newStore(t *testing.T) testStore {
	t.Helper()
	kv := keyval_testutil.New(t)
	encKV, err := keyval.NewEncrypted(kv, encrypt.TestKey)
	require.NoError(t, err)
	s, err := mcpgateway.NewStore(
		db_testutil.NewDB(t, mcpgateway.Models()),
		encKV, kv, encrypt.TestKey, fakeOrgs{}, http.DefaultClient, callbackURL,
	)
	require.NoError(t, err)
	return testStore{Store: s, kv: kv}
}

// connect runs did through authorizing with server's OAuth server name.
func connect(
	t *testing.T,
	s mcpgateway.Store,
	fake *mcpgatewaytest.Server,
	did syntax.DID,
	name mcpgateway.ServerName,
) {
	t.Helper()
	authURL, err := s.StartAuthorization(t.Context(), did, org, name)
	require.NoError(t, err)
	q := fake.Authorize(t, authURL)
	done, err := s.CompleteAuthorization(t.Context(), q.Get("state"), q.Get("code"), q.Get("iss"))
	require.NoError(t, err)
	require.Equal(t, &mcpgateway.CompletedAuthorization{OrgID: org, Server: name, DID: did}, done)
}

func TestNewStoreValidation(t *testing.T) {
	kv := keyval_testutil.New(t)
	encKV, err := keyval.NewEncrypted(kv, encrypt.TestKey)
	require.NoError(t, err)
	db := db_testutil.NewDB(t, mcpgateway.Models())
	_, err = mcpgateway.NewStore(
		db,
		encKV,
		kv,
		[]byte("short"),
		fakeOrgs{},
		http.DefaultClient,
		callbackURL,
	)
	require.Error(t, err)
	_, err = mcpgateway.NewStore(
		db,
		encKV,
		kv,
		encrypt.TestKey,
		nil,
		http.DefaultClient,
		callbackURL,
	)
	require.Error(t, err)
	_, err = mcpgateway.NewStore(db, encKV, kv, encrypt.TestKey, fakeOrgs{}, nil, callbackURL)
	require.Error(t, err)
	_, err = mcpgateway.NewStore(
		db,
		encKV,
		kv,
		encrypt.TestKey,
		fakeOrgs{},
		http.DefaultClient,
		"/cb",
	)
	require.Error(t, err)
}

func TestAddServerValidation(t *testing.T) {
	s := newStore(t)
	_, err := s.AddServer(t.Context(), org, mcpgateway.NewServer{
		Name: "docs", URL: "https://docs.example.com/mcp", AuthType: mcpgateway.AuthTypeManual,
	})
	require.NoError(t, err)

	for name, tc := range map[string]struct {
		server mcpgateway.NewServer
		want   error
	}{
		"bad name": {
			mcpgateway.NewServer{Name: "a:b", URL: "https://x.example.com", AuthType: "manual"},
			mcpgateway.ErrInvalidServerName,
		},
		"bad url": {
			mcpgateway.NewServer{Name: "x", URL: "not a url", AuthType: "manual"},
			mcpgateway.ErrInvalidServerURL,
		},
		"url with credentials": {
			mcpgateway.NewServer{Name: "x", URL: "https://u:p@x.example.com", AuthType: "manual"},
			mcpgateway.ErrInvalidServerURL,
		},
		"bad auth type": {
			mcpgateway.NewServer{Name: "x", URL: "https://x.example.com", AuthType: "magic"},
			mcpgateway.ErrInvalidAuthType,
		},
		"taken": {
			mcpgateway.NewServer{Name: "docs", URL: "https://x.example.com", AuthType: "manual"},
			mcpgateway.ErrServerNameTaken,
		},
		"secret without id": {
			mcpgateway.NewServer{
				Name: "x", URL: "https://x.example.com", AuthType: "oauth", ClientSecret: "s",
			},
			mcpgateway.ErrClientSecretWithoutID,
		},
		"client for manual": {
			mcpgateway.NewServer{
				Name: "x", URL: "https://x.example.com", AuthType: "manual", ClientID: "c",
			},
			mcpgateway.ErrClientNotSupported,
		},
		"preset with url": {
			mcpgateway.NewServer{
				Name: "x", Preset: mcpgateway.PresetSlack, URL: "https://x.example.com",
			},
			mcpgateway.ErrPresetConflict,
		},
		"preset with client": {
			mcpgateway.NewServer{Name: "x", Preset: mcpgateway.PresetSlack, ClientID: "c"},
			mcpgateway.ErrPresetConflict,
		},
		"unknown preset": {
			mcpgateway.NewServer{Name: "x", Preset: "nope"},
			mcpgateway.ErrUnknownPreset,
		},
		"preset without a client": {
			mcpgateway.NewServer{Name: "x", Preset: mcpgateway.PresetGitHub},
			mcpgateway.ErrPresetNotConfigured,
		},
		"unreachable oauth": {
			mcpgateway.NewServer{Name: "x", URL: "http://127.0.0.1:1/mcp", AuthType: "oauth"},
			mcpgateway.ErrOAuthDiscovery,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.AddServer(t.Context(), org, tc.server)
			require.ErrorIs(t, err, tc.want)
			require.True(t, mcpgateway.IsConfigError(err))
		})
	}
}

func TestManualServer(t *testing.T) {
	s := newStore(t)
	added, err := s.AddServer(t.Context(), org, mcpgateway.NewServer{
		Name: "docs", Description: "Docs", URL: "https://docs.example.com/mcp",
		AuthType: mcpgateway.AuthTypeManual,
	})
	require.NoError(t, err)
	require.Equal(t, &mcpgateway.Server{
		OrgID: org, Name: "docs", Description: "Docs", AuthType: mcpgateway.AuthTypeManual,
		URL: "https://docs.example.com/mcp",
	}, added)

	// Every member is connected, with nothing to authorize.
	listed, err := s.ListServers(t.Context(), org, alice)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.True(t, listed[0].Connected)
	_, err = s.StartAuthorization(t.Context(), alice, org, "docs")
	require.ErrorIs(t, err, mcpgateway.ErrManualServer)
	require.ErrorIs(
		t,
		s.DisconnectServer(t.Context(), alice, org, "docs"),
		mcpgateway.ErrManualServer,
	)

	d, err := s.Downstream(t.Context(), alice, org, "docs")
	require.NoError(t, err)
	require.Equal(t, "https://docs.example.com/mcp", d.URL)
	require.Empty(t, d.Headers)
	require.NotNil(t, d.Client)

	// Its URL can change.
	newURL, desc := "https://docs2.example.com/mcp", "New docs"
	updated, err := s.UpdateServer(t.Context(), org, "docs", mcpgateway.ServerUpdate{
		URL: &newURL, Description: &desc,
	})
	require.NoError(t, err)
	require.Equal(t, newURL, updated.URL)
	require.Equal(t, desc, updated.Description)
	badURL := "nope"
	_, err = s.UpdateServer(t.Context(), org, "docs", mcpgateway.ServerUpdate{URL: &badURL})
	require.ErrorIs(t, err, mcpgateway.ErrInvalidServerURL)

	connected, err := s.ListConnectedServers(t.Context(), alice)
	require.NoError(t, err)
	require.Len(t, connected, 1)
	connected, err = s.ListConnectedServers(t.Context(), nobody)
	require.NoError(t, err)
	require.Empty(t, connected)

	require.NoError(t, s.RemoveServer(t.Context(), org, "docs"))
	require.ErrorIs(t, s.RemoveServer(t.Context(), org, "docs"), mcpgateway.ErrServerNotFound)
	_, err = s.UpdateServer(t.Context(), org, "docs", mcpgateway.ServerUpdate{})
	require.ErrorIs(t, err, mcpgateway.ErrServerNotFound)
	_, err = s.Downstream(t.Context(), alice, org, "docs")
	require.ErrorIs(t, err, mcpgateway.ErrServerNotFound)
}

func TestOAuthServerConnectFlow(t *testing.T) {
	fake := mcpgatewaytest.New(t)
	s := newStore(t)
	added, err := s.AddServer(t.Context(), org, mcpgateway.NewServer{
		Name: "linear", URL: fake.URL, AuthType: mcpgateway.AuthTypeOAuth,
	})
	require.NoError(t, err)
	require.Equal(t, mcpgateway.AuthTypeOAuth, added.AuthType)

	listed, err := s.ListServers(t.Context(), org, alice)
	require.NoError(t, err)
	require.False(t, listed[0].Connected)
	_, err = s.Downstream(t.Context(), alice, org, "linear")
	require.ErrorIs(t, err, mcpgateway.ErrNotConnected)
	require.ErrorIs(
		t,
		s.DisconnectServer(t.Context(), alice, org, "linear"),
		mcpgateway.ErrNotConnected,
	)

	connect(t, s, fake, alice, "linear")

	// Only alice is connected.
	listed, err = s.ListServers(t.Context(), org, alice)
	require.NoError(t, err)
	require.True(t, listed[0].Connected)
	listed, err = s.ListServers(t.Context(), org, bob)
	require.NoError(t, err)
	require.False(t, listed[0].Connected)
	connected, err := s.ListConnectedServers(t.Context(), alice)
	require.NoError(t, err)
	require.Len(t, connected, 1)
	connected, err = s.ListConnectedServers(t.Context(), bob)
	require.NoError(t, err)
	require.Empty(t, connected)

	d, err := s.Downstream(t.Context(), alice, org, "linear")
	require.NoError(t, err)
	require.Equal(t, fake.URL, d.URL)
	require.Contains(t, d.Headers["Authorization"], "Bearer at-")

	// An OAuth server's URL is fixed.
	other := "https://other.example.com/mcp"
	_, err = s.UpdateServer(t.Context(), org, "linear", mcpgateway.ServerUpdate{URL: &other})
	require.ErrorIs(t, err, mcpgateway.ErrURLImmutable)

	require.NoError(t, s.DisconnectServer(t.Context(), alice, org, "linear"))
	_, err = s.Downstream(t.Context(), alice, org, "linear")
	require.ErrorIs(t, err, mcpgateway.ErrNotConnected)
}

func TestCompleteAuthorizationRejectsBadResponses(t *testing.T) {
	fake := mcpgatewaytest.New(t)
	s := newStore(t)
	_, err := s.AddServer(t.Context(), org, mcpgateway.NewServer{
		Name: "linear", URL: fake.URL, AuthType: mcpgateway.AuthTypeOAuth,
	})
	require.NoError(t, err)

	_, err = s.CompleteAuthorization(t.Context(), "", "code", "")
	require.ErrorIs(t, err, mcpgateway.ErrAuthorizationNotFound)
	_, err = s.CompleteAuthorization(t.Context(), "unknown", "code", "")
	require.ErrorIs(t, err, mcpgateway.ErrAuthorizationNotFound)

	start := func() (state, code string) {
		authURL, err := s.StartAuthorization(t.Context(), alice, org, "linear")
		require.NoError(t, err)
		q := fake.Authorize(t, authURL)
		return q.Get("state"), q.Get("code")
	}

	// The fake advertises iss, so a response without it is refused.
	state, code := start()
	_, err = s.CompleteAuthorization(t.Context(), state, code, "")
	require.ErrorIs(t, err, mcpgateway.ErrAuthorizationFailed)
	// And the state was used up.
	_, err = s.CompleteAuthorization(t.Context(), state, code, fake.Issuer)
	require.ErrorIs(t, err, mcpgateway.ErrAuthorizationNotFound)

	state, code = start()
	_, err = s.CompleteAuthorization(t.Context(), state, code, "https://evil.example.com")
	require.ErrorIs(t, err, mcpgateway.ErrAuthorizationFailed)

	state, _ = start()
	_, err = s.CompleteAuthorization(t.Context(), state, "", fake.Issuer)
	require.ErrorIs(t, err, mcpgateway.ErrAuthorizationFailed)

	state, _ = start()
	_, err = s.CompleteAuthorization(t.Context(), state, "wrong-code", fake.Issuer)
	require.ErrorIs(t, err, mcpgateway.ErrAuthorizationFailed)

	// Removing the server while a member authorizes voids the authorization.
	state, code = start()
	require.NoError(t, s.RemoveServer(t.Context(), org, "linear"))
	_, err = s.CompleteAuthorization(t.Context(), state, code, fake.Issuer)
	require.ErrorIs(t, err, mcpgateway.ErrServerNotFound)
}

func TestRemovedServerTokensAreNotReused(t *testing.T) {
	fake := mcpgatewaytest.New(t)
	s := newStore(t)
	add := func() {
		_, err := s.AddServer(t.Context(), org, mcpgateway.NewServer{
			Name: "linear", URL: fake.URL, AuthType: mcpgateway.AuthTypeOAuth,
		})
		require.NoError(t, err)
	}
	add()
	connect(t, s, fake, alice, "linear")
	require.NoError(t, s.RemoveServer(t.Context(), org, "linear"))

	// Re-added under the same name, alice must connect again.
	add()
	listed, err := s.ListServers(t.Context(), org, alice)
	require.NoError(t, err)
	require.False(t, listed[0].Connected)
	_, err = s.Downstream(t.Context(), alice, org, "linear")
	require.ErrorIs(t, err, mcpgateway.ErrNotConnected)
}

func TestDownstreamRefreshesExpiringTokens(t *testing.T) {
	// Shorter than the store's refresh skew, so every token is refreshed.
	fake := mcpgatewaytest.New(t, mcpgatewaytest.WithAccessTokenTTL(30*time.Second))
	s := newStore(t)
	_, err := s.AddServer(t.Context(), org, mcpgateway.NewServer{
		Name: "linear", URL: fake.URL, AuthType: mcpgateway.AuthTypeOAuth,
	})
	require.NoError(t, err)
	connect(t, s, fake, alice, "linear")

	first, err := s.Downstream(t.Context(), alice, org, "linear")
	require.NoError(t, err)
	require.Equal(t, 1, fake.Refreshes)
	// The rotated refresh token was stored, so the next refresh works too.
	second, err := s.Downstream(t.Context(), alice, org, "linear")
	require.NoError(t, err)
	require.Equal(t, 2, fake.Refreshes)
	require.NotEqual(t, first.Headers["Authorization"], second.Headers["Authorization"])

	// A revoked grant disconnects alice.
	fake.RevokeRefreshTokens()
	_, err = s.Downstream(t.Context(), alice, org, "linear")
	require.ErrorIs(t, err, mcpgateway.ErrNotConnected)
	listed, err := s.ListServers(t.Context(), org, alice)
	require.NoError(t, err)
	require.False(t, listed[0].Connected)
}

func TestPreregisteredClient(t *testing.T) {
	fake := mcpgatewaytest.New(t, mcpgatewaytest.WithoutDCR())
	s := newStore(t)
	_, err := s.AddServer(t.Context(), org, mcpgateway.NewServer{
		Name: "linear", URL: fake.URL, AuthType: mcpgateway.AuthTypeOAuth,
	})
	require.ErrorIs(t, err, mcpgateway.ErrOAuthDiscovery)

	clientID := fake.RegisterClient(callbackURL)
	_, err = s.AddServer(t.Context(), org, mcpgateway.NewServer{
		Name: "linear", URL: fake.URL, AuthType: mcpgateway.AuthTypeOAuth,
		ClientID: clientID, ClientSecret: "shh",
	})
	require.NoError(t, err)
	connect(t, s, fake, alice, "linear")
}
