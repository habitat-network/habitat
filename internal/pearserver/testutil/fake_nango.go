package testutil

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// FakeNangoClient is an in-memory stand-in for internal/nango.Client,
// satisfying internal/mcpgateway.NangoClient, so tests don't need real Nango
// credentials or network access.
type FakeNangoClient struct {
	Integrations map[string]bool
	Connections  map[string]string // connectionID -> uniqueKey
}

// NewFakeNangoClient constructs an empty FakeNangoClient.
func NewFakeNangoClient() *FakeNangoClient {
	return &FakeNangoClient{
		Integrations: make(map[string]bool),
		Connections:  make(map[string]string),
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
	return "session-token-" + uuid.NewString(), nil
}

func (f *FakeNangoClient) DeleteConnection(ctx context.Context, connectionID, providerConfigKey string) error {
	if f.Connections[connectionID] != providerConfigKey {
		return errors.New("connection not found")
	}
	delete(f.Connections, connectionID)
	return nil
}
