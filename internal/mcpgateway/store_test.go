package mcpgateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/stretchr/testify/require"
)

// fakeNangoClient is an in-memory stand-in for internal/nango.Client.
type fakeNangoClient struct {
	integrations         map[string]bool
	connections          map[string]string // connectionID -> uniqueKey
	createIntegrationErr error
	createSessionErr     error
}

func newFakeNangoClient() *fakeNangoClient {
	return &fakeNangoClient{
		integrations: make(map[string]bool),
		connections:  make(map[string]string),
	}
}

func (f *fakeNangoClient) CreateIntegration(ctx context.Context, uniqueKey string) error {
	if f.createIntegrationErr != nil {
		return f.createIntegrationErr
	}
	f.integrations[uniqueKey] = true
	return nil
}

func (f *fakeNangoClient) DeleteIntegration(ctx context.Context, uniqueKey string) error {
	if !f.integrations[uniqueKey] {
		return errors.New("integration not found")
	}
	delete(f.integrations, uniqueKey)
	return nil
}

func (f *fakeNangoClient) CreateConnectSession(
	ctx context.Context, uniqueKey string, endUserID, orgID string,
) (string, error) {
	if f.createSessionErr != nil {
		return "", f.createSessionErr
	}
	if !f.integrations[uniqueKey] {
		return "", errors.New("unknown integration")
	}
	return "session-token-" + uuid.NewString(), nil
}

func (f *fakeNangoClient) DeleteConnection(ctx context.Context, connectionID, providerConfigKey string) error {
	if f.connections[connectionID] != providerConfigKey {
		return errors.New("connection not found")
	}
	delete(f.connections, connectionID)
	return nil
}

func newTestStore(t *testing.T) (Store, *fakeNangoClient) {
	t.Helper()
	nangoClient := newFakeNangoClient()
	s, err := NewStore(testutil.NewDB(t), http.DefaultClient, nangoClient)
	require.NoError(t, err)
	return s, nangoClient
}

func newFakeOpenMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newFakeOAuthMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://example.com/.well-known/oauth-protected-resource"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStoreAddServer_DetectsNoAuth(t *testing.T) {
	s, nangoClient := newTestStore(t)
	srv := newFakeOpenMCPServer(t)

	server, err := s.AddServer(t.Context(), syntax.DID("did:plc:org"), "Open", srv.URL, "")
	require.NoError(t, err)
	require.Equal(t, AuthTypeNone, server.AuthType)
	require.Empty(t, nangoClient.integrations)
}

func TestStoreAddServer_DetectsOAuthAndCreatesIntegration(t *testing.T) {
	s, nangoClient := newTestStore(t)
	fake := newFakeOAuthMCPServer(t)

	server, err := s.AddServer(t.Context(), syntax.DID("did:plc:org"), "Linear", fake.URL, "")
	require.NoError(t, err)
	require.Equal(t, AuthTypeOAuth, server.AuthType)
	require.True(t, nangoClient.integrations[string(server.ID)])
}

func TestStoreAddServer_ListsForOrgOnly(t *testing.T) {
	s, _ := newTestStore(t)
	orgA := syntax.DID("did:plc:org-a")
	orgB := syntax.DID("did:plc:org-b")
	srv := newFakeOpenMCPServer(t)

	server, err := s.AddServer(t.Context(), orgA, "Open", srv.URL, "")
	require.NoError(t, err)
	require.NotEmpty(t, server.ID)

	otherSrv := newFakeOpenMCPServer(t)
	_, err = s.AddServer(t.Context(), orgB, "Other", otherSrv.URL, "")
	require.NoError(t, err)

	servers, err := s.ListServers(t.Context(), orgA)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, server.ID, servers[0].ID)
}

func TestStoreUpdateServer(t *testing.T) {
	s, _ := newTestStore(t)
	org := syntax.DID("did:plc:org")
	srv := newFakeOpenMCPServer(t)

	server, err := s.AddServer(t.Context(), org, "Open", srv.URL, "desc")
	require.NoError(t, err)

	newName := "Open MCP"
	updated, err := s.UpdateServer(t.Context(), org, server.ID, &newName, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "Open MCP", updated.Name)
	require.Equal(t, srv.URL, updated.URL) // unchanged

	// Wrong org can't update it.
	_, err = s.UpdateServer(t.Context(), syntax.DID("did:plc:other"), server.ID, &newName, nil, nil)
	require.ErrorIs(t, err, ErrServerNotFound)
}

func TestStoreUpdateServer_URLChangeCreatesIntegration(t *testing.T) {
	s, nangoClient := newTestStore(t)
	org := syntax.DID("did:plc:org")
	openSrv := newFakeOpenMCPServer(t)

	server, err := s.AddServer(t.Context(), org, "Server", openSrv.URL, "")
	require.NoError(t, err)
	require.Equal(t, AuthTypeNone, server.AuthType)

	oauthSrv := newFakeOAuthMCPServer(t)
	newURL := oauthSrv.URL
	updated, err := s.UpdateServer(t.Context(), org, server.ID, nil, &newURL, nil)
	require.NoError(t, err)
	require.Equal(t, AuthTypeOAuth, updated.AuthType)
	require.True(t, nangoClient.integrations[string(server.ID)])
}

func TestStoreRemoveServer_CascadesConnectionsAndIntegration(t *testing.T) {
	s, nangoClient := newTestStore(t)
	org := syntax.DID("did:plc:org")
	did := syntax.DID("did:plc:user")
	fake := newFakeOAuthMCPServer(t)

	server, err := s.AddServer(t.Context(), org, "Linear", fake.URL, "")
	require.NoError(t, err)

	nangoClient.connections["conn-1"] = string(server.ID)
	require.NoError(t, s.ConfirmConnection(t.Context(), did, org, server.ID, "conn-1"))
	connected, err := s.IsConnected(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.True(t, connected)

	require.NoError(t, s.RemoveServer(t.Context(), org, server.ID))

	_, err = s.GetServer(t.Context(), org, server.ID)
	require.ErrorIs(t, err, ErrServerNotFound)
	require.False(t, nangoClient.integrations[string(server.ID)])

	connected, err = s.IsConnected(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.False(t, connected)
}

func TestStoreRemoveServer_NotFound(t *testing.T) {
	s, _ := newTestStore(t)
	err := s.RemoveServer(t.Context(), syntax.DID("did:plc:org"), ServerID("nonexistent"))
	require.ErrorIs(t, err, ErrServerNotFound)
}

func TestStoreStartAuthorization_NotOAuthServer(t *testing.T) {
	s, _ := newTestStore(t)
	org := syntax.DID("did:plc:org")
	srv := newFakeOpenMCPServer(t)

	server, err := s.AddServer(t.Context(), org, "Open", srv.URL, "")
	require.NoError(t, err)

	_, err = s.StartAuthorization(t.Context(), syntax.DID("did:plc:user"), org, server.ID)
	require.ErrorIs(t, err, ErrNotOAuthServer)
}

func TestStoreAuthorizationFlow(t *testing.T) {
	s, nangoClient := newTestStore(t)
	org := syntax.DID("did:plc:org")
	did := syntax.DID("did:plc:user")
	fake := newFakeOAuthMCPServer(t)

	server, err := s.AddServer(t.Context(), org, "Linear", fake.URL, "")
	require.NoError(t, err)

	connected, err := s.IsConnected(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.False(t, connected)

	sessionToken, err := s.StartAuthorization(t.Context(), did, org, server.ID)
	require.NoError(t, err)
	require.NotEmpty(t, sessionToken)

	nangoClient.connections["conn-1"] = string(server.ID)
	require.NoError(t, s.ConfirmConnection(t.Context(), did, org, server.ID, "conn-1"))

	connected, err = s.IsConnected(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.True(t, connected)
}

func TestStoreDisconnectServer(t *testing.T) {
	s, nangoClient := newTestStore(t)
	org := syntax.DID("did:plc:org")
	did := syntax.DID("did:plc:user")
	fake := newFakeOAuthMCPServer(t)

	server, err := s.AddServer(t.Context(), org, "Linear", fake.URL, "")
	require.NoError(t, err)
	nangoClient.connections["conn-1"] = string(server.ID)
	require.NoError(t, s.ConfirmConnection(t.Context(), did, org, server.ID, "conn-1"))

	require.NoError(t, s.DisconnectServer(t.Context(), did, server.ID))
	connected, err := s.IsConnected(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.False(t, connected)
	require.NotContains(t, nangoClient.connections, "conn-1")

	err = s.DisconnectServer(t.Context(), did, server.ID)
	require.ErrorIs(t, err, ErrCredentialNotFound)
}

func TestStoreIsConnected_NotFound(t *testing.T) {
	s, _ := newTestStore(t)
	connected, err := s.IsConnected(t.Context(), syntax.DID("did:plc:user"), ServerID("none"))
	require.NoError(t, err)
	require.False(t, connected)
}
