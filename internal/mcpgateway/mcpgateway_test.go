package mcpgateway_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/habitat-network/habitat/internal/mcpgateway"
)

// oneServer is a ConnectedServers reaching a single server, or failing to
// with err.
type oneServer struct {
	downstream *mcpgateway.Downstream
	err        error
}

func (o oneServer) ListConnectedServers(
	context.Context, syntax.DID, syntax.DID,
) ([]*mcpgateway.Server, error) {
	return []*mcpgateway.Server{{Name: "docs"}}, nil
}

func (o oneServer) Downstream(
	context.Context, syntax.DID, syntax.DID, mcpgateway.ServerName,
) (*mcpgateway.Downstream, error) {
	return o.downstream, o.err
}

// newPingServer serves an MCP server with a "ping" tool to requests carrying
// "Bearer secret".
func newPingServer(t *testing.T) *httptest.Server {
	t.Helper()
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
	return httpServer
}

func TestWithSessionSendsHeaders(t *testing.T) {
	httpServer := newPingServer(t)
	server := &mcpgateway.Server{Name: "docs"}
	servers := oneServer{downstream: &mcpgateway.Downstream{
		URL:     httpServer.URL,
		Headers: map[string]string{"Authorization": "Bearer secret"},
		Client:  httpServer.Client(),
	}}
	var res *mcp.CallToolResult
	err := mcpgateway.WithSession(t.Context(), servers, "did:plc:alice", server,
		func(ctx context.Context, session *mcp.ClientSession) (err error) {
			res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "ping"})
			return err
		})
	require.NoError(t, err)
	require.Equal(t, []mcp.Content{&mcp.TextContent{Text: "pong"}}, res.Content)

	err = mcpgateway.WithSession(
		t.Context(), oneServer{downstream: &mcpgateway.Downstream{URL: httpServer.URL}},
		"did:plc:alice", server,
		func(context.Context, *mcp.ClientSession) error { return nil },
	)
	require.Error(t, err, "without the headers the server refuses the session")
}

// The session's context keeps the caller's cancellation and trace span, but
// none of its values (see sessionContext).
func TestWithSessionContext(t *testing.T) {
	httpServer := newPingServer(t)
	servers := oneServer{downstream: &mcpgateway.Downstream{
		URL:     httpServer.URL,
		Headers: map[string]string{"Authorization": "Bearer secret"},
	}}
	type key struct{}
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1},
		SpanID:  trace.SpanID{1},
	})
	parent, cancel := context.WithCancel(
		trace.ContextWithSpanContext(context.WithValue(t.Context(), key{}, "v"), spanCtx),
	)
	defer cancel()
	err := mcpgateway.WithSession(
		parent,
		servers,
		"did:plc:alice",
		&mcpgateway.Server{Name: "docs"},
		func(ctx context.Context, _ *mcp.ClientSession) error {
			require.Nil(t, ctx.Value(key{}))
			require.Equal(t, spanCtx, trace.SpanContextFromContext(ctx))
			cancel()
			<-ctx.Done()
			return ctx.Err()
		},
	)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWithSessionNotConnected(t *testing.T) {
	called := false
	err := mcpgateway.WithSession(
		t.Context(), oneServer{err: mcpgateway.ErrNotConnected},
		"did:plc:alice", &mcpgateway.Server{Name: "docs"},
		func(context.Context, *mcp.ClientSession) error { called = true; return nil },
	)
	require.ErrorIs(t, err, mcpgateway.ErrNotConnected)
	require.ErrorContains(t, err, "docs")
	require.False(t, called)
}

func TestForEach(t *testing.T) {
	items := make([]int, 20)
	for i := range items {
		items[i] = i * 2
	}
	out := make([]int, len(items))
	mcpgateway.ForEach(t.Context(), items, 0, func(ctx context.Context, i int, item int) {
		_, hasDeadline := ctx.Deadline()
		require.False(t, hasDeadline)
		out[i] = item
	})
	require.Equal(t, items, out)
}

// One slow item is cut off at the timeout without holding up the others.
func TestForEachTimeout(t *testing.T) {
	timedOut := make([]bool, 2)
	mcpgateway.ForEach(t.Context(), []bool{true, false}, 20*time.Millisecond,
		func(ctx context.Context, i int, hang bool) {
			if hang {
				<-ctx.Done()
			}
			timedOut[i] = errors.Is(ctx.Err(), context.DeadlineExceeded)
		})
	require.Equal(t, []bool{true, false}, timedOut)
}

func TestPresetURL(t *testing.T) {
	require.Equal(t, "https://mcp.slack.com/mcp", mcpgateway.PresetURL(mcpgateway.PresetSlack))
	require.Empty(t, mcpgateway.PresetURL("unknown"))
}
