package mcpserver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/fgastore"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/ipfs/go-cid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
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

// setupStores builds a spaces.Store and a perms.Store sharing the same
// throwaway DB and an in-memory FGA store, mirroring how cmd/pear/main.go
// wires them together.
func setupStores(t *testing.T) (spaces.Store, perms.Store) {
	t.Helper()
	db := pear_testutil.NewPearDB(t)
	spacesStore := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db))
	osTestStore := opensocial_testutil.NewTestStore(
		t,
		opensocial_testutil.WithDB(db),
		opensocial_testutil.WithSpaceStore(spacesStore),
	)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	permStore := perms.NewStore(db, spacesStore, fga, osTestStore.Store)
	return spacesStore, permStore
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
	spacesStore, permStore := setupStores(t)

	owner := syntax.DID("did:plc:owner")
	other := syntax.DID("did:plc:other")
	collection := syntax.NSID("network.habitat.example")

	spaceURI, err := spacesStore.CreateSpace(
		ctx, owner, syntax.NSID("network.habitat.space.type"), habitat_syntax.SpaceKey("test"),
	)
	require.NoError(t, err)

	recordURI, _, err := spacesStore.PutRecord(
		ctx, spaceURI, owner, collection, "abc123",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"hello": "world"}),
	)
	require.NoError(t, err)

	srv := New(
		fakeTokens{},
		spacesStore,
		permStore,
		nil,
		newFakeNangoClient(),
		newFakeOrgMcpServerStore(),
		nil,
		nil,
		"https://habitat.example",
		"https://habitat.example/mcp",
	)
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	t.Run("owner can read their own record", func(t *testing.T) {
		session, err := connectAs(t, ctx, httpServer.URL, owner.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_record",
			Arguments: map[string]any{"uri": recordURI.String()},
		})
		require.NoError(t, err)
		require.False(t, result.IsError, "%+v", result)
		require.Equal(t, map[string]any{
			"uri":   recordURI.String(),
			"value": map[string]any{"hello": "world"},
		}, result.StructuredContent)
	})

	t.Run("tool also returns an embedded resource", func(t *testing.T) {
		session, err := connectAs(t, ctx, httpServer.URL, owner.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_record",
			Arguments: map[string]any{"uri": recordURI.String()},
		})
		require.NoError(t, err)
		require.Len(t, result.Content, 1)
		embedded, ok := result.Content[0].(*mcp.EmbeddedResource)
		require.True(t, ok)
		require.Equal(t, &mcp.ResourceContents{
			URI:      recordResourceURI(recordURI.String()),
			MIMEType: "application/json",
			Text:     `{"hello":"world"}`,
		}, embedded.Resource)
	})

	t.Run("record resource is readable by the owner only", func(t *testing.T) {
		uri := recordResourceURI(recordURI.String())
		session, err := connectAs(t, ctx, httpServer.URL, owner.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()
		res, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		require.NoError(t, err)
		require.Equal(t, []*mcp.ResourceContents{{
			URI: uri, MIMEType: "application/json", Text: `{"hello":"world"}`,
		}}, res.Contents)

		otherSession, err := connectAs(t, ctx, httpServer.URL, other.String())
		require.NoError(t, err)
		defer func() { _ = otherSession.Close() }()
		_, err = otherSession.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		require.Error(t, err)
	})

	t.Run("non-owner without a grant is rejected", func(t *testing.T) {
		session, err := connectAs(t, ctx, httpServer.URL, other.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_record",
			Arguments: map[string]any{"uri": recordURI.String()},
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

// fakeRecordSearcher records the last search it ran and returns canned matches.
type fakeRecordSearcher struct {
	caller  syntax.DID
	query   search.Query
	matches []search.Match
	err     error
}

func (f *fakeRecordSearcher) Search(
	_ context.Context, caller syntax.DID, q search.Query,
) (search.Page, error) {
	f.caller, f.query = caller, q
	return search.Page{Matches: f.matches}, f.err
}

func TestMCPServerSearchRecordsTool(t *testing.T) {
	ctx := t.Context()
	spacesStore, permStore := setupStores(t)

	owner := syntax.DID("did:plc:owner")
	org := syntax.DID("did:plc:org")
	collection := syntax.NSID("network.habitat.example")

	spaceURI, err := spacesStore.CreateSpace(
		ctx, owner, syntax.NSID("network.habitat.space.type"), habitat_syntax.SpaceKey("test"),
	)
	require.NoError(t, err)
	recordURI, recordCid, err := spacesStore.PutRecord(
		ctx, spaceURI, owner, collection, "abc123",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"hello": "world"}),
	)
	require.NoError(t, err)

	searcher := &fakeRecordSearcher{
		matches: []search.Match{
			{
				URI: recordURI,
				Record: spaces.Record{
					Space: spaceURI,
					Value: map[string]any{"hello": "world"},
					Cid:   *recordCid,
				},
				Snippet: "<mark>hello</mark>",
			},
		},
	}
	newHTTPServer := func(s RecordSearcher) *httptest.Server {
		srv := New(
			fakeTokens{}, spacesStore, permStore, s,
			newFakeNangoClient(), newFakeOrgMcpServerStore(), nil,
			nil,
			"https://habitat.example", "https://habitat.example/mcp",
		)
		return httptest.NewServer(srv.Handler())
	}
	httpServer := newHTTPServer(searcher)
	defer httpServer.Close()

	t.Run("searches as the caller", func(t *testing.T) {
		session, err := connectAs(t, ctx, httpServer.URL, owner.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "search_records",
			Arguments: map[string]any{
				"query": "hello", "org": org.String(),
				"collection": collection.String(), "limit": 5,
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError, "%+v", result)
		require.Equal(t, owner, searcher.caller)
		require.Equal(t, search.Query{
			Text: "hello", Org: org, Collections: []syntax.NSID{collection}, Limit: 5,
		}, searcher.query)

		out, ok := result.StructuredContent.(map[string]any)
		require.True(t, ok)
		hits, ok := out["hits"].([]any)
		require.True(t, ok)
		require.Len(t, hits, 1)
		hit, ok := hits[0].(map[string]any)
		require.True(t, ok)
		require.Equal(t, recordURI.String(), hit["uri"])
		require.Equal(t, map[string]any{"hello": "world"}, hit["value"])
		require.Equal(t, "<mark>hello</mark>", hit["snippet"])
	})

	t.Run("invalid input is rejected", func(t *testing.T) {
		session, err := connectAs(t, ctx, httpServer.URL, owner.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()

		for _, args := range []map[string]any{
			{"query": ""},
			{"query": "x", "org": "not-a-did"},
			{"query": "x", "collection": "nope"},
			{"query": "x", "limit": 101},
		} {
			result, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name: "search_records", Arguments: args,
			})
			require.NoError(t, err)
			require.True(t, result.IsError, "%v", args)
		}
	})

	t.Run("tool is absent when search is not configured", func(t *testing.T) {
		noSearch := newHTTPServer(nil)
		defer noSearch.Close()
		session, err := connectAs(t, ctx, noSearch.URL, owner.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()

		tools, err := session.ListTools(ctx, nil)
		require.NoError(t, err)
		for _, tool := range tools.Tools {
			require.NotEqual(t, "search_records", tool.Name)
		}
	})
}

func TestMCPServerGetBlobTool(t *testing.T) {
	ctx := t.Context()
	spacesStore, permStore := setupStores(t)
	blobs := spaces_testutil.NewTestBlobStore(t)

	owner := syntax.DID("did:plc:owner")
	other := syntax.DID("did:plc:other")
	spaceURI, err := spacesStore.CreateSpace(
		ctx, owner, syntax.NSID("network.habitat.space.type"), habitat_syntax.SpaceKey("test"),
	)
	require.NoError(t, err)

	textCid, _, err := blobs.PutBlob(ctx, "text/plain; charset=utf-8", []byte("hello blob"))
	require.NoError(t, err)
	binData := []byte{0x00, 0x01, 0xfe, 0xff}
	binCid, _, err := blobs.PutBlob(ctx, "image/png", binData)
	require.NoError(t, err)
	bigCid, _, err := blobs.PutBlob(
		ctx, "text/plain", bytes.Repeat([]byte("a"), maxBlobContentBytes+1),
	)
	require.NoError(t, err)
	missingCid, err := cid.Decode("bafkreigh2akiscaildcqabsyg3dfr6chu3fgpregiymsck7e7aqa4s52zy")
	require.NoError(t, err)

	srv := New(
		fakeTokens{}, spacesStore, permStore, nil,
		newFakeNangoClient(), newFakeOrgMcpServerStore(), nil, blobs,
		"https://habitat.example", "https://habitat.example/mcp",
	)
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	callTool := func(t *testing.T, caller syntax.DID, space string, c string) *mcp.CallToolResult {
		t.Helper()
		session, err := connectAs(t, ctx, httpServer.URL, caller.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_blob",
			Arguments: map[string]any{"space": space, "cid": c},
		})
		require.NoError(t, err)
		return result
	}
	readResource := func(t *testing.T, caller syntax.DID, uri string) (*mcp.ReadResourceResult, error) {
		t.Helper()
		session, err := connectAs(t, ctx, httpServer.URL, caller.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()
		return session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	}

	t.Run("resource read returns text", func(t *testing.T) {
		uri := blobResourceURI(spaceURI, textCid)
		res, err := readResource(t, owner, uri)
		require.NoError(t, err)
		require.Equal(t, []*mcp.ResourceContents{{
			URI: uri, MIMEType: "text/plain; charset=utf-8", Text: "hello blob",
		}}, res.Contents)
	})

	t.Run("resource read returns binary", func(t *testing.T) {
		uri := blobResourceURI(spaceURI, binCid)
		res, err := readResource(t, owner, uri)
		require.NoError(t, err)
		require.Equal(t, []*mcp.ResourceContents{{
			URI: uri, MIMEType: "image/png", Blob: binData,
		}}, res.Contents)
	})

	t.Run("resource read rejects oversized, ungranted and missing blobs", func(t *testing.T) {
		_, err := readResource(t, owner, blobResourceURI(spaceURI, bigCid))
		require.Error(t, err)
		_, err = readResource(t, other, blobResourceURI(spaceURI, textCid))
		require.Error(t, err)
		_, err = readResource(t, owner, blobResourceURI(spaceURI, missingCid))
		var rpcErr *jsonrpc.Error
		require.ErrorAs(t, err, &rpcErr)
		require.Equal(t, int64(jsonrpc.CodeInvalidParams), rpcErr.Code)
	})

	t.Run("tool returns an embedded resource", func(t *testing.T) {
		result := callTool(t, owner, spaceURI.String(), binCid.String())
		require.False(t, result.IsError, "%+v", result)
		require.Len(t, result.Content, 1)
		embedded, ok := result.Content[0].(*mcp.EmbeddedResource)
		require.True(t, ok)
		require.Equal(t, &mcp.ResourceContents{
			URI: blobResourceURI(spaceURI, binCid), MIMEType: "image/png", Blob: binData,
		}, embedded.Resource)

		result = callTool(t, owner, spaceURI.String(), textCid.String())
		require.False(t, result.IsError, "%+v", result)
		embedded, ok = result.Content[0].(*mcp.EmbeddedResource)
		require.True(t, ok)
		require.Equal(t, "hello blob", embedded.Resource.Text)
	})

	t.Run("tool rejects ungranted, invalid, oversized and missing", func(t *testing.T) {
		require.True(t, callTool(t, other, spaceURI.String(), textCid.String()).IsError)
		require.True(t, callTool(t, owner, "not-a-uri", textCid.String()).IsError)
		require.True(t, callTool(t, owner, spaceURI.String(), "not-a-cid").IsError)
		require.True(t, callTool(t, owner, spaceURI.String(), bigCid.String()).IsError)
		require.True(t, callTool(t, owner, spaceURI.String(), missingCid.String()).IsError)
	})
}
