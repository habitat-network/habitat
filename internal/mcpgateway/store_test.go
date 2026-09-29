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
	// lastSessionServerURL is the serverURL the last CreateConnectSession
	// call pre-filled.
	lastSessionServerURL string
}

// fakeServerURL is the MCP server URL fakeNangoClient reports for
// connectionID.
func fakeServerURL(connectionID string) string {
	return "https://mcp.example.com/" + connectionID
}

func (f *fakeNangoClient) GetConnection(
	ctx context.Context, connectionID, providerConfigKey string,
) (*nango.ConnectionDetails, error) {
	conn, ok := f.connections[connectionID]
	if !ok || conn.ProviderConfigKey != providerConfigKey {
		return nil, errors.New("connection not found")
	}
	return &nango.ConnectionDetails{MCPServerURL: fakeServerURL(connectionID)}, nil
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
	ctx context.Context, uniqueKey string, endUserID, orgID, serverURL string,
) (string, error) {
	if f.createSessionErr != nil {
		return "", f.createSessionErr
	}
	f.lastSessionServerURL = serverURL
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

func (f *fakeNangoClient) DeleteConnection(
	ctx context.Context,
	connectionID, providerConfigKey string,
) error {
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

// newTestMember provisions did as a member of org.
func newTestMember(t *testing.T, records OrgMcpServerStore, org syntax.DID) syntax.DID {
	t.Helper()
	ts, ok := records.(*opensocial_testutil.TestStore)
	require.True(t, ok)
	did := syntax.DID("did:plc:member-" + uuid.NewString())
	require.NoError(t, ts.ProvisionMember(t.Context(), org, did))
	return did
}

// addServer adds an OAuth server via Store.AddServer and returns it.
func addServer(
	t *testing.T,
	s Store,
	org syntax.DID,
	name, description string,
) *Server {
	t.Helper()
	server, err := s.AddServer(
		t.Context(),
		org,
		name,
		description,
		"https://mcp.example.com/mcp",
		AuthTypeOAuth,
	)
	require.NoError(t, err)
	return server
}

// connectMember simulates member completing the Nango Connect UI for
// server, as the frontend's StartAuthorization flow would.
func connectMember(
	nangoClient *fakeNangoClient,
	org syntax.DID,
	server *Server,
	member syntax.DID,
) {
	nangoClient.connect("conn-"+string(server.ID), NangoKeyFor(org, server.ID), member.String())
}

func TestStoreAddServer_CreatesIntegration(t *testing.T) {
	s, nangoClient, records := newTestStore(t)
	org := newTestOrg(t, records)

	server, err := s.AddServer(
		t.Context(), org, "linear", "desc", "https://mcp.example.com/mcp", AuthTypeOAuth,
	)
	require.NoError(t, err)
	require.Equal(t, "linear", server.Name)
	require.Equal(t, "desc", server.Description)
	require.Equal(t, AuthTypeOAuth, server.AuthType)
	require.True(t, nangoClient.integrations[NangoKeyFor(org, server.ID)])

	// No one is signed in yet, but the record already exists.
	all, err := records.ListMcpServers(t.Context(), org)
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, "https://mcp.example.com/mcp", all[0].ServerURL)
}

func TestStoreAddServer_RejectsInvalidName(t *testing.T) {
	s, _, records := newTestStore(t)
	org := newTestOrg(t, records)

	_, err := s.AddServer(
		t.Context(), org, "not a valid name!", "", "https://a.example", AuthTypeOAuth,
	)
	require.ErrorIs(t, err, ErrInvalidServerName)
}

func TestStoreAddServer_RejectsInvalidURL(t *testing.T) {
	s, _, records := newTestStore(t)
	org := newTestOrg(t, records)

	_, err := s.AddServer(t.Context(), org, "srv", "", "ftp://a.example", AuthTypeOAuth)
	require.ErrorIs(t, err, ErrInvalidServerURL)
}

func TestStoreAddServer_RejectsInvalidAuthType(t *testing.T) {
	s, _, records := newTestStore(t)
	org := newTestOrg(t, records)

	_, err := s.AddServer(t.Context(), org, "srv", "", "https://a.example", AuthType("bogus"))
	require.ErrorIs(t, err, ErrInvalidAuthType)
}

func TestStoreAddServer_RejectsDuplicateName(t *testing.T) {
	s, _, records := newTestStore(t)
	org := newTestOrg(t, records)

	addServer(t, s, org, "linear", "")

	_, err := s.AddServer(t.Context(), org, "linear", "", "https://a.example", AuthTypeOAuth)
	require.ErrorIs(t, err, ErrServerNameTaken)

	_, err = s.AddServer(t.Context(), org, "linear", "", "https://a.example", AuthTypeManual)
	require.ErrorIs(t, err, ErrServerNameTaken)
}

func TestStoreStartAuthorization_PrefillsServerURL(t *testing.T) {
	s, nangoClient, records := newTestStore(t)
	org := newTestOrg(t, records)

	server := addServer(t, s, org, "linear", "")
	_, err := s.StartAuthorization(t.Context(), "did:plc:member", org, server.ID)
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example.com/mcp", nangoClient.lastSessionServerURL)
}

func TestStoreListServers_ScopedPerOrg(t *testing.T) {
	s, _, records := newTestStore(t)
	orgA := newTestOrg(t, records)
	orgB := newTestOrg(t, records)

	server := addServer(t, s, orgA, "server-a", "")
	addServer(t, s, orgB, "server-b", "")

	servers, err := s.ListServers(t.Context(), orgA, syntax.DID("did:plc:user"))
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, server.ID, servers[0].Server.ID)
}

func TestStoreUpdateServer(t *testing.T) {
	s, _, records := newTestStore(t)
	org := newTestOrg(t, records)

	server := addServer(t, s, org, "server", "desc")

	newDescription := "updated description"
	updated, err := s.UpdateServer(
		t.Context(), org, server.ID, ServerUpdate{Description: &newDescription},
	)
	require.NoError(t, err)
	require.Equal(t, "server", updated.Name) // unchanged
	require.Equal(t, "updated description", updated.Description)
}

func TestStoreRemoveServer_DeletesIntegration(t *testing.T) {
	s, nangoClient, records := newTestStore(t)
	org := newTestOrg(t, records)
	did := syntax.DID("did:plc:user")

	server := addServer(t, s, org, "linear", "")

	require.NoError(t, s.RemoveServer(t.Context(), org, server.ID))

	servers, err := s.ListServers(t.Context(), org, did)
	require.NoError(t, err)
	require.Empty(t, servers)
	require.False(t, nangoClient.integrations[NangoKeyFor(org, server.ID)])
}

func TestStoreListServers_ReflectsNangoConnections(t *testing.T) {
	s, nangoClient, records := newTestStore(t)
	org := newTestOrg(t, records)
	member := syntax.DID("did:plc:member")

	server := addServer(t, s, org, "linear", "")

	servers, err := s.ListServers(t.Context(), org, member)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.False(t, servers[0].Connected)

	sessionToken, err := s.StartAuthorization(t.Context(), member, org, server.ID)
	require.NoError(t, err)
	require.NotEmpty(t, sessionToken)

	// The Nango Connect UI reports success directly to the frontend, which
	// simply refetches; there's nothing for the gateway to record.
	connectMember(nangoClient, org, server, member)

	servers, err = s.ListServers(t.Context(), org, member)
	require.NoError(t, err)
	require.True(t, servers[0].Connected)
}

func TestStoreDisconnectServer(t *testing.T) {
	s, nangoClient, records := newTestStore(t)
	org := newTestOrg(t, records)
	did := syntax.DID("did:plc:user")

	server := addServer(t, s, org, "linear", "")
	connectMember(nangoClient, org, server, did)

	require.NoError(t, s.DisconnectServer(t.Context(), did, org, server.ID))
	require.NotContains(t, nangoClient.connections, "conn-"+string(server.ID))

	err := s.DisconnectServer(t.Context(), did, org, server.ID)
	require.ErrorIs(t, err, ErrNotConnected)
}

func TestStoreAddManualServer(t *testing.T) {
	s, _, records := newTestStore(t)
	org := newTestOrg(t, records)
	member := syntax.DID("did:plc:member")

	server, err := s.AddServer(
		t.Context(), org, "docs", "internal docs", "https://mcp.example.com/mcp", AuthTypeManual,
	)
	require.NoError(t, err)
	require.Equal(t, AuthTypeManual, server.AuthType)
	require.Equal(t, "docs", server.Name)

	// Manual servers are connected for every member without any sign-in.
	servers, err := s.ListServers(t.Context(), org, member)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.True(t, servers[0].Connected)
	require.Equal(t, AuthTypeManual, servers[0].Server.AuthType)

	_, err = s.StartAuthorization(t.Context(), member, org, server.ID)
	require.ErrorIs(t, err, ErrManualServer)
	require.ErrorIs(t, s.DisconnectServer(t.Context(), member, org, server.ID), ErrManualServer)
}

func TestStoreUpdateManualServer(t *testing.T) {
	s, _, records := newTestStore(t)
	org := newTestOrg(t, records)
	creator := newTestMember(t, records, org)

	_, err := s.AddServer(
		t.Context(),
		org,
		"docs",
		"old",
		"https://old.example/mcp",
		AuthTypeManual,
	)
	require.NoError(t, err)

	newURL := "https://new.example/mcp"
	newDescription := "new"
	updated, err := s.UpdateServer(t.Context(), org, "docs", ServerUpdate{
		Description: &newDescription,
		URL:         &newURL,
	})
	require.NoError(t, err)
	require.Equal(t, "new", updated.Description)

	manual, err := s.ListManualServersForMember(t.Context(), creator)
	require.NoError(t, err)
	require.Len(t, manual, 1)
	require.Equal(t, newURL, manual[0].URL)

	// An invalid URL is rejected.
	badURL := "not a url"
	_, err = s.UpdateServer(t.Context(), org, "docs", ServerUpdate{URL: &badURL})
	require.ErrorIs(t, err, ErrInvalidServerURL)

	// URL doesn't apply to OAuth servers.
	oauth := addServer(t, s, org, "linear", "")
	_, err = s.UpdateServer(t.Context(), org, oauth.ID, ServerUpdate{URL: &newURL})
	require.Error(t, err)
}

func TestStoreRemoveManualServer(t *testing.T) {
	s, _, records := newTestStore(t)
	org := newTestOrg(t, records)

	_, err := s.AddServer(t.Context(), org, "docs", "", "https://a.example", AuthTypeManual)
	require.NoError(t, err)
	require.NoError(t, s.RemoveServer(t.Context(), org, "docs"))

	servers, err := s.ListServers(t.Context(), org, "did:plc:member")
	require.NoError(t, err)
	require.Empty(t, servers)
	require.ErrorIs(t, s.RemoveServer(t.Context(), org, "docs"), opensocial.ErrMcpServerNotFound)
}

func TestStoreListManualServersForMember_OnlyMemberOrgs(t *testing.T) {
	s, _, records := newTestStore(t)
	org := newTestOrg(t, records)

	_, err := s.AddServer(t.Context(), org, "docs", "", "https://a.example", AuthTypeManual)
	require.NoError(t, err)

	manual, err := s.ListManualServersForMember(t.Context(), newTestMember(t, records, org))
	require.NoError(t, err)
	require.Len(t, manual, 1)
	require.Equal(t, org, manual[0].OrgID)

	manual, err = s.ListManualServersForMember(t.Context(), "did:plc:stranger")
	require.NoError(t, err)
	require.Empty(t, manual)
}
