package mcpgateway_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/mcpgateway"
)

func TestDownstreamConnectSendsHeaders(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "downstream", Version: "0.1.0"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "ping"}, func(
		context.Context, *mcp.CallToolRequest, struct{},
	) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "pong"}}}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{DisableLocalhostProtection: true},
	)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)

	d := &mcpgateway.Downstream{
		URL:     httpServer.URL,
		Headers: map[string]string{"Authorization": "Bearer secret"},
		Client:  httpServer.Client(),
	}
	session, err := d.Connect(t.Context())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "ping"})
	require.NoError(t, err)
	require.Equal(t, []mcp.Content{&mcp.TextContent{Text: "pong"}}, res.Content)

	_, err = (&mcpgateway.Downstream{URL: httpServer.URL}).Connect(t.Context())
	require.Error(t, err, "without the headers the server refuses the session")
}

func TestPresetURL(t *testing.T) {
	require.Equal(t, "https://mcp.slack.com/mcp", mcpgateway.PresetURL(mcpgateway.PresetSlack))
	require.Empty(t, mcpgateway.PresetURL("unknown"))
}
