package mcpgateway

import (
	"context"
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/nango"
	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
)

// fakeConnection is a nango.Connection plus the end user it belongs to,
// mirroring how the real API scopes ListConnections by the end_user_id tag.
type fakeConnection struct {
	nango.Connection
	endUserID string
}

// fakeNangoClient is an in-memory stand-in for internal/nango.Client,
// satisfying NangoClient, so tests don't need real Nango credentials or
// network access.
type fakeNangoClient struct {
	integrations         map[string]bool
	connections          map[string]fakeConnection // connectionID -> connection
	createIntegrationErr error
	createSessionErr     error
}

func newFakeNangoClient() *fakeNangoClient {
	return &fakeNangoClient{
		integrations: make(map[string]bool),
		connections:  make(map[string]fakeConnection),
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
	return "session-token", nil
}

// connect simulates endUserID completing the Nango Connect UI for uniqueKey,
// as a test setup helper.
func (f *fakeNangoClient) connect(connectionID, uniqueKey, endUserID string) {
	f.connections[connectionID] = fakeConnection{
		Connection: nango.Connection{
			ConnectionID:      connectionID,
			ProviderConfigKey: uniqueKey,
		},
		endUserID: endUserID,
	}
}

func (f *fakeNangoClient) ListConnections(
	ctx context.Context, endUserID string,
) ([]nango.Connection, error) {
	conns := make([]nango.Connection, 0, len(f.connections))
	for _, c := range f.connections {
		if c.endUserID == endUserID {
			conns = append(conns, c.Connection)
		}
	}
	return conns, nil
}

func (f *fakeNangoClient) DeleteConnection(ctx context.Context, connectionID, providerConfigKey string) error {
	if f.connections[connectionID].ProviderConfigKey != providerConfigKey {
		return errors.New("connection not found")
	}
	delete(f.connections, connectionID)
	return nil
}

func newTestStore(t *testing.T) (Store, *fakeNangoClient, OrgMcpServerStore) {
	t.Helper()
	nangoClient := newFakeNangoClient()
	records := opensocial_testutil.NewTestStore(t)
	s, err := NewStore(nangoClient, records)
	require.NoError(t, err)
	return s, nangoClient, records
}

func newTestOrg(t *testing.T, records OrgMcpServerStore) syntax.DID {
	t.Helper()
	ts, ok := records.(*opensocial_testutil.TestStore)
	require.True(t, ok)
	handle := "acme-" + uuid.NewString()
	orgDID, err := ts.NewOrg(t.Context(), handle, syntax.DID("did:plc:creator"))
	require.NoError(t, err)
	return syntax.DID(orgDID)
}

// addServer drives the full BeginAddServer -> connect -> CompleteAddServer
// flow, as the frontend would, and returns the resulting server.
func addServer(
	t *testing.T, s Store, nangoClient *fakeNangoClient, org syntax.DID, did syntax.DID, name, description string,
) *opensocial.McpServer {
	t.Helper()
	id, sessionToken, err := s.BeginAddServer(t.Context(), org, did, name, description)
	require.NoError(t, err)
	require.NotEmpty(t, sessionToken)
	nangoKey := NangoKeyFor(org, id)
	require.True(t, nangoClient.integrations[nangoKey])

	nangoClient.connect("conn-"+string(id), nangoKey, did.String())

	server, err := s.CompleteAddServer(t.Context(), org, did, id, name, description)
	require.NoError(t, err)
	return server
}

func TestStoreBeginAddServer_CreatesIntegrationAndSession(t *testing.T) {
	s, nangoClient, records := newTestStore(t)
	org := newTestOrg(t, records)
	did := syntax.DID("did:plc:admin")

	id, sessionToken, err := s.BeginAddServer(t.Context(), org, did, "linear", "")
	require.NoError(t, err)
	require.NotEmpty(t, id)
	require.NotEmpty(t, sessionToken)
	require.True(t, nangoClient.integrations[NangoKeyFor(org, id)])

	// No record exists yet: begin alone doesn't write one.
	servers, err := records.ListMcpServers(t.Context(), org)
	require.NoError(t, err)
	require.Empty(t, servers)
}

func TestStoreCompleteAddServer_RequiresNangoConnection(t *testing.T) {
	s, _, records := newTestStore(t)
	org := newTestOrg(t, records)
	did := syntax.DID("did:plc:admin")

	id, _, err := s.BeginAddServer(t.Context(), org, did, "linear", "")
	require.NoError(t, err)

	_, err = s.CompleteAddServer(t.Context(), org, did, id, "linear", "")
	require.Error(t, err)
}

func TestStoreCompleteAddServer_WritesRecord(t *testing.T) {
	s, nangoClient, records := newTestStore(t)
	org := newTestOrg(t, records)
	did := syntax.DID("did:plc:admin")

	server := addServer(t, s, nangoClient, org, did, "linear", "desc")
	require.Equal(t, "linear", server.Name)
	require.Equal(t, "desc", server.Description)

	all, err := records.ListMcpServers(t.Context(), org)
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, server.ID, all[0].ID)
}

func TestStoreCancelAddServer_DeletesIntegration(t *testing.T) {
	s, nangoClient, records := newTestStore(t)
	org := newTestOrg(t, records)
	did := syntax.DID("did:plc:admin")

	id, _, err := s.BeginAddServer(t.Context(), org, did, "linear", "")
	require.NoError(t, err)
	require.True(t, nangoClient.integrations[NangoKeyFor(org, id)])

	require.NoError(t, s.CancelAddServer(t.Context(), org, id))
	require.False(t, nangoClient.integrations[NangoKeyFor(org, id)])
}

func TestStoreListServers_ScopedPerOrg(t *testing.T) {
	s, nangoClient, records := newTestStore(t)
	orgA := newTestOrg(t, records)
	orgB := newTestOrg(t, records)
	did := syntax.DID("did:plc:admin")

	server := addServer(t, s, nangoClient, orgA, did, "server-a", "")
	addServer(t, s, nangoClient, orgB, did, "server-b", "")

	servers, err := s.ListServers(t.Context(), orgA, syntax.DID("did:plc:user"))
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, server.ID, servers[0].Server.ID)
}

func TestStoreUpdateServer(t *testing.T) {
	s, nangoClient, records := newTestStore(t)
	org := newTestOrg(t, records)
	did := syntax.DID("did:plc:admin")

	server := addServer(t, s, nangoClient, org, did, "server", "desc")

	newDescription := "updated description"
	updated, err := s.UpdateServer(t.Context(), org, server.ID, &newDescription)
	require.NoError(t, err)
	require.Equal(t, "server", updated.Name) // unchanged
	require.Equal(t, "updated description", updated.Description)
}

func TestStoreBeginAddServer_RejectsInvalidName(t *testing.T) {
	s, _, records := newTestStore(t)
	org := newTestOrg(t, records)
	did := syntax.DID("did:plc:admin")

	_, _, err := s.BeginAddServer(t.Context(), org, did, "not a valid name!", "")
	require.ErrorIs(t, err, ErrInvalidServerName)
}

func TestStoreBeginAddServer_RejectsDuplicateName(t *testing.T) {
	s, nangoClient, records := newTestStore(t)
	org := newTestOrg(t, records)
	did := syntax.DID("did:plc:admin")

	addServer(t, s, nangoClient, org, did, "linear", "")

	_, _, err := s.BeginAddServer(t.Context(), org, did, "linear", "")
	require.ErrorIs(t, err, ErrServerNameTaken)
}

func TestStoreRemoveServer_DeletesIntegration(t *testing.T) {
	s, nangoClient, records := newTestStore(t)
	org := newTestOrg(t, records)
	did := syntax.DID("did:plc:user")

	server := addServer(t, s, nangoClient, org, did, "linear", "")

	require.NoError(t, s.RemoveServer(t.Context(), org, server.ID))

	servers, err := s.ListServers(t.Context(), org, did)
	require.NoError(t, err)
	require.Empty(t, servers)
	require.False(t, nangoClient.integrations[NangoKeyFor(org, server.ID)])
}

func TestStoreListServers_ReflectsNangoConnections(t *testing.T) {
	s, nangoClient, records := newTestStore(t)
	org := newTestOrg(t, records)
	admin := syntax.DID("did:plc:admin")
	member := syntax.DID("did:plc:member")

	server := addServer(t, s, nangoClient, org, admin, "linear", "")

	servers, err := s.ListServers(t.Context(), org, member)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.False(t, servers[0].Connected)

	sessionToken, err := s.StartAuthorization(t.Context(), member, org, server.ID)
	require.NoError(t, err)
	require.NotEmpty(t, sessionToken)

	// The Nango Connect UI reports success directly to the frontend, which
	// simply refetches; there's nothing for the gateway to record.
	nangoClient.connect("conn-member", NangoKeyFor(org, server.ID), member.String())

	servers, err = s.ListServers(t.Context(), org, member)
	require.NoError(t, err)
	require.True(t, servers[0].Connected)
}

func TestStoreDisconnectServer(t *testing.T) {
	s, nangoClient, records := newTestStore(t)
	org := newTestOrg(t, records)
	did := syntax.DID("did:plc:user")

	server := addServer(t, s, nangoClient, org, did, "linear", "")

	require.NoError(t, s.DisconnectServer(t.Context(), did, org, server.ID))
	require.NotContains(t, nangoClient.connections, "conn-"+string(server.ID))

	err := s.DisconnectServer(t.Context(), did, org, server.ID)
	require.ErrorIs(t, err, ErrNotConnected)
}
