package mcpgateway

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	s, err := NewStore(testutil.NewDB(t), encrypt.TestKey)
	require.NoError(t, err)
	return s
}

func TestStoreAddServer_ListsForOrgOnly(t *testing.T) {
	s := newTestStore(t)
	orgA := syntax.DID("did:plc:org-a")
	orgB := syntax.DID("did:plc:org-b")

	server, err := s.AddServer(t.Context(), orgA, "Linear", "https://mcp.linear.app", "", AuthTypeAPIKey)
	require.NoError(t, err)
	require.NotEmpty(t, server.ID)
	require.Equal(t, "Linear", server.Name)
	require.Equal(t, orgA, server.OrgID)

	_, err = s.AddServer(t.Context(), orgB, "Other", "https://example.com", "", AuthTypeNone)
	require.NoError(t, err)

	servers, err := s.ListServers(t.Context(), orgA)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, server.ID, servers[0].ID)
}

func TestStoreAddServer_InvalidAuthType(t *testing.T) {
	s := newTestStore(t)
	_, err := s.AddServer(t.Context(), syntax.DID("did:plc:org"), "X", "https://x.example", "", "bogus")
	require.ErrorIs(t, err, ErrInvalidAuthType)
}

func TestStoreUpdateServer(t *testing.T) {
	s := newTestStore(t)
	org := syntax.DID("did:plc:org")

	server, err := s.AddServer(t.Context(), org, "Linear", "https://mcp.linear.app", "desc", AuthTypeAPIKey)
	require.NoError(t, err)

	newName := "Linear MCP"
	updated, err := s.UpdateServer(t.Context(), org, server.ID, &newName, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "Linear MCP", updated.Name)
	require.Equal(t, "https://mcp.linear.app", updated.URL) // unchanged

	// Wrong org can't update it.
	_, err = s.UpdateServer(t.Context(), syntax.DID("did:plc:other"), server.ID, &newName, nil, nil, nil)
	require.ErrorIs(t, err, ErrServerNotFound)
}

func TestStoreRemoveServer_CascadesCredentials(t *testing.T) {
	s := newTestStore(t)
	org := syntax.DID("did:plc:org")
	did := syntax.DID("did:plc:user")

	server, err := s.AddServer(t.Context(), org, "Linear", "https://mcp.linear.app", "", AuthTypeAPIKey)
	require.NoError(t, err)

	require.NoError(t, s.ConnectServer(t.Context(), did, server.ID, "secret-key"))
	connected, err := s.IsConnected(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.True(t, connected)

	require.NoError(t, s.RemoveServer(t.Context(), org, server.ID))

	_, err = s.GetServer(t.Context(), org, server.ID)
	require.ErrorIs(t, err, ErrServerNotFound)

	_, err = s.GetCredential(t.Context(), did, server.ID)
	require.ErrorIs(t, err, ErrCredentialNotFound)
}

func TestStoreRemoveServer_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.RemoveServer(t.Context(), syntax.DID("did:plc:org"), ServerID("nonexistent"))
	require.ErrorIs(t, err, ErrServerNotFound)
}

func TestStoreConnectDisconnect(t *testing.T) {
	s := newTestStore(t)
	org := syntax.DID("did:plc:org")
	did := syntax.DID("did:plc:user")

	server, err := s.AddServer(t.Context(), org, "Linear", "https://mcp.linear.app", "", AuthTypeAPIKey)
	require.NoError(t, err)

	connected, err := s.IsConnected(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.False(t, connected)

	require.NoError(t, s.ConnectServer(t.Context(), did, server.ID, "secret-key"))

	cred, err := s.GetCredential(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.Equal(t, "secret-key", cred)

	// Reconnecting replaces the credential.
	require.NoError(t, s.ConnectServer(t.Context(), did, server.ID, "new-secret"))
	cred, err = s.GetCredential(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.Equal(t, "new-secret", cred)

	require.NoError(t, s.DisconnectServer(t.Context(), did, server.ID))
	connected, err = s.IsConnected(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.False(t, connected)

	err = s.DisconnectServer(t.Context(), did, server.ID)
	require.ErrorIs(t, err, ErrCredentialNotFound)
}

func TestStoreGetCredential_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetCredential(t.Context(), syntax.DID("did:plc:user"), ServerID("none"))
	require.ErrorIs(t, err, ErrCredentialNotFound)
}
