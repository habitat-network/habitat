package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/clique"
	dbtestutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// fakeTokens is an authn.RawMethod fake standing in for
// oauthserver.OAuthServer: a token validates as the DID equal to the token
// string itself, and any other token (including empty) is invalid.
type fakeTokens struct{}

func (fakeTokens) ValidateRaw(
	_ context.Context,
	token string,
	_ ...string,
) (*authn.CredentialInfo, bool, error) {
	if token == "" || token == "invalid" {
		return nil, false, nil
	}
	return &authn.CredentialInfo{Subject: syntax.DID(token)}, true, nil
}

// bearerTransport adds a static bearer token to every outgoing request, so
// tests can drive the MCP client as a specific authenticated caller.
type bearerTransport struct {
	token string
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func setupStores(t *testing.T) (repo.Repo, permissions.Store) {
	t.Helper()
	db := dbtestutil.NewDB(t)
	cliqueStore, err := clique.NewStore(db)
	require.NoError(t, err)
	permStore, err := permissions.NewStore(db, cliqueStore)
	require.NoError(t, err)
	repoStore, err := repo.NewRepo(db)
	require.NoError(t, err)
	return repoStore, permStore
}

func connectAs(
	t *testing.T,
	ctx context.Context,
	serverURL string,
	token string,
) (*mcp.ClientSession, error) {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	return client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   serverURL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: token}},
	}, nil)
}

func TestMCPServerGetRecordTool(t *testing.T) {
	ctx := t.Context()
	repoStore, permStore := setupStores(t)

	owner := syntax.DID("did:plc:owner")
	other := syntax.DID("did:plc:other")

	uri, err := repoStore.PutRecord(ctx, repo.Record{
		Did:        owner.String(),
		Collection: "network.habitat.example",
		Rkey:       "abc123",
		Value:      map[string]any{"hello": "world"},
	}, nil)
	require.NoError(t, err)

	srv := New(fakeTokens{}, repoStore, permStore, "https://habitat.example")
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	t.Run("owner can read their own record", func(t *testing.T) {
		session, err := connectAs(t, ctx, httpServer.URL, owner.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_record",
			Arguments: map[string]any{"uri": uri.String()},
		})
		require.NoError(t, err)
		require.False(t, result.IsError, "%+v", result)
		require.Equal(t, map[string]any{
			"uri":   uri.String(),
			"value": map[string]any{"hello": "world"},
		}, result.StructuredContent)
	})

	t.Run("non-owner without a grant is rejected", func(t *testing.T) {
		session, err := connectAs(t, ctx, httpServer.URL, other.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_record",
			Arguments: map[string]any{"uri": uri.String()},
		})
		require.NoError(t, err)
		require.True(t, result.IsError)
	})

	t.Run("invalid uri is rejected", func(t *testing.T) {
		session, err := connectAs(t, ctx, httpServer.URL, owner.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_record",
			Arguments: map[string]any{"uri": "not-a-uri"},
		})
		require.NoError(t, err)
		require.True(t, result.IsError)
	})

	t.Run("unauthenticated request is rejected", func(t *testing.T) {
		_, err := connectAs(t, ctx, httpServer.URL, "")
		require.Error(t, err)
	})
}
