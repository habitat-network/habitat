package mcpgateway_test

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/mcpgateway"
)

func TestDebug(t *testing.T) {
	httpServer := newPingServer(t)
	server := &mcpgateway.Server{Name: "docs"}
	servers := oneServer{downstream: &mcpgateway.Downstream{
		URL:     httpServer.URL,
		Headers: map[string]string{"Authorization": "Bearer secret"},
		Client:  httpServer.Client(),
	}}

	t.Run("lists tools", func(t *testing.T) {
		res, err := mcpgateway.Debug(t.Context(), servers, "did:plc:alice", server,
			mcpgateway.DebugRequest{Method: mcpgateway.DebugToolsList})
		require.NoError(t, err)
		require.Empty(t, res.Err)
		tools, ok := res.Result.(*mcp.ListToolsResult)
		require.True(t, ok)
		require.Equal(t, "ping", tools.Tools[0].Name)
	})

	t.Run("calls a tool", func(t *testing.T) {
		res, err := mcpgateway.Debug(t.Context(), servers, "did:plc:alice", server,
			mcpgateway.DebugRequest{Method: mcpgateway.DebugToolsCall, Name: "ping"})
		require.NoError(t, err)
		require.Empty(t, res.Err)
		require.IsType(t, &mcp.CallToolResult{}, res.Result)
	})

	t.Run("reports a protocol error raw", func(t *testing.T) {
		res, err := mcpgateway.Debug(t.Context(), servers, "did:plc:alice", server,
			mcpgateway.DebugRequest{Method: mcpgateway.DebugResourcesRead, URI: "file:///nope"})
		require.NoError(t, err)
		require.Equal(t, mcpgateway.DebugProtocol, res.ErrKind)
		require.Contains(t, res.Err, "JSON-RPC error")
		require.Nil(t, res.Result)
	})

	t.Run("reports a connection that isn't signed in", func(t *testing.T) {
		res, err := mcpgateway.Debug(t.Context(),
			oneServer{err: mcpgateway.ErrNotConnected}, "did:plc:alice", server,
			mcpgateway.DebugRequest{Method: mcpgateway.DebugToolsList})
		require.NoError(t, err)
		require.Equal(t, mcpgateway.DebugNotConnected, res.ErrKind)
	})

	t.Run("reports a failed connection", func(t *testing.T) {
		res, err := mcpgateway.Debug(t.Context(),
			oneServer{downstream: &mcpgateway.Downstream{URL: httpServer.URL}},
			"did:plc:alice", server, mcpgateway.DebugRequest{Method: mcpgateway.DebugToolsList})
		require.NoError(t, err)
		require.NotEmpty(t, res.Err)
		require.Equal(t, mcpgateway.DebugTransport, res.ErrKind)
	})

	t.Run("rejects malformed requests", func(t *testing.T) {
		for _, req := range []mcpgateway.DebugRequest{
			{Method: "bogus"},
			{Method: mcpgateway.DebugToolsCall},
			{Method: mcpgateway.DebugResourcesRead},
		} {
			_, err := mcpgateway.Debug(t.Context(), servers, "did:plc:alice", server, req)
			require.ErrorIs(t, err, mcpgateway.ErrInvalidDebugRequest)
		}
	})
}
