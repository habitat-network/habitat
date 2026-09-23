package testutil

import (
	"context"
	"errors"

	"github.com/habitat-network/habitat/internal/nango"
)

// fakeConnection is a nango.Connection plus the end user it belongs to,
// mirroring how the real API scopes ListConnections by the end_user_id tag.
type fakeConnection struct {
	nango.Connection
	endUserID string
}

// FakeNangoClient is an in-memory stand-in for internal/nango.Client,
// satisfying internal/mcpgateway.NangoClient, so tests don't need real Nango
// credentials or network access.
type FakeNangoClient struct {
	Integrations map[string]bool
	Connections  map[string]fakeConnection // connectionID -> connection
}

// NewFakeNangoClient constructs an empty FakeNangoClient.
func NewFakeNangoClient() *FakeNangoClient {
	return &FakeNangoClient{
		Integrations: make(map[string]bool),
		Connections:  make(map[string]fakeConnection),
	}
}

func (f *FakeNangoClient) CreateIntegration(ctx context.Context, uniqueKey string) error {
	f.Integrations[uniqueKey] = true
	return nil
}

func (f *FakeNangoClient) DeleteIntegration(ctx context.Context, uniqueKey string) error {
	delete(f.Integrations, uniqueKey)
	return nil
}

func (f *FakeNangoClient) CreateConnectSession(
	ctx context.Context, uniqueKey string, endUserID, orgID string,
) (string, error) {
	if !f.Integrations[uniqueKey] {
		return "", errors.New("unknown integration")
	}
	return "session-token", nil
}

// Connect simulates endUserID completing the Nango Connect UI for uniqueKey,
// as a test setup helper.
func (f *FakeNangoClient) Connect(connectionID, uniqueKey, endUserID string) {
	f.Connections[connectionID] = fakeConnection{
		Connection: nango.Connection{
			ConnectionID:      connectionID,
			ProviderConfigKey: uniqueKey,
		},
		endUserID: endUserID,
	}
}

func (f *FakeNangoClient) ListConnections(
	ctx context.Context, endUserID string,
) ([]nango.Connection, error) {
	conns := make([]nango.Connection, 0, len(f.Connections))
	for _, c := range f.Connections {
		if c.endUserID == endUserID {
			conns = append(conns, c.Connection)
		}
	}
	return conns, nil
}

func (f *FakeNangoClient) DeleteConnection(ctx context.Context, connectionID, providerConfigKey string) error {
	if f.Connections[connectionID].ProviderConfigKey != providerConfigKey {
		return errors.New("connection not found")
	}
	delete(f.Connections, connectionID)
	return nil
}
