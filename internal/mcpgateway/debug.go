package mcpgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DebugMethod is a raw MCP request Debug can make.
type DebugMethod string

const (
	DebugToolsList     DebugMethod = "tools/list"
	DebugResourcesList DebugMethod = "resources/list"
	DebugPromptsList   DebugMethod = "prompts/list"
	DebugToolsCall     DebugMethod = "tools/call"
	DebugResourcesRead DebugMethod = "resources/read"
)

// DebugErrorKind says where a Debug request failed.
type DebugErrorKind string

const (
	// DebugNotConnected: the caller has no usable connection to the server.
	DebugNotConnected DebugErrorKind = "not_connected"
	// DebugProtocol: the server answered with a JSON-RPC error.
	DebugProtocol DebugErrorKind = "protocol"
	// DebugTransport: connecting or the HTTP exchange failed.
	DebugTransport DebugErrorKind = "transport"
	// DebugTimeout: the server didn't answer before the deadline.
	DebugTimeout DebugErrorKind = "timeout"
	// DebugInvalidResponse: the server's reply didn't decode or validate
	// against the MCP schema (for example invalid structuredContent).
	DebugInvalidResponse DebugErrorKind = "invalid_response"
)

// ErrInvalidDebugRequest is returned by Debug for an unknown method, or one
// missing the field it needs.
var ErrInvalidDebugRequest = errors.New("invalid debug request")

// DebugRequest is one raw MCP request to make to a member's server.
type DebugRequest struct {
	Method DebugMethod
	// Name and Arguments are the tool to call, for DebugToolsCall.
	Name      string
	Arguments any
	// URI is the resource to read, for DebugResourcesRead.
	URI string
}

// DebugResult is how a Debug request went. Result is the server's answer, as
// the MCP client decoded it, when Err is empty.
type DebugResult struct {
	Result     any
	Err        string
	ErrKind    DebugErrorKind
	DurationMs int64
}

// Debug makes req to server on did's behalf and reports exactly what came
// back, including the raw error if it failed, instead of returning it. It's
// for diagnosing a server's configuration, so unlike the rest of the gateway
// it does not paper over a misbehaving server. It returns an error only for a
// malformed req.
func Debug(
	ctx context.Context,
	servers ConnectedServers,
	did syntax.DID,
	server *Server,
	req DebugRequest,
) (*DebugResult, error) {
	switch req.Method {
	case DebugToolsCall:
		if req.Name == "" {
			return nil, fmt.Errorf("%w: tools/call needs a tool name", ErrInvalidDebugRequest)
		}
	case DebugResourcesRead:
		if req.URI == "" {
			return nil, fmt.Errorf("%w: resources/read needs a uri", ErrInvalidDebugRequest)
		}
	case DebugToolsList, DebugResourcesList, DebugPromptsList:
	default:
		return nil, fmt.Errorf("%w: unknown method %q", ErrInvalidDebugRequest, req.Method)
	}

	start := time.Now()
	var result any
	err := WithSession(ctx, servers, did, server,
		func(ctx context.Context, session *mcp.ClientSession) error {
			var err error
			switch req.Method {
			case DebugToolsList:
				result, err = session.ListTools(ctx, nil)
			case DebugResourcesList:
				result, err = session.ListResources(ctx, nil)
			case DebugPromptsList:
				result, err = session.ListPrompts(ctx, nil)
			case DebugToolsCall:
				result, err = session.CallTool(ctx, &mcp.CallToolParams{
					Name: req.Name, Arguments: req.Arguments,
				})
			case DebugResourcesRead:
				result, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: req.URI})
			}
			return err
		})
	out := &DebugResult{DurationMs: time.Since(start).Milliseconds()}
	if err != nil {
		out.Err, out.ErrKind = describeDebugError(ctx, err)
		return out, nil
	}
	out.Result = result
	return out, nil
}

// describeDebugError renders err as the raw text a developer needs and says
// where it came from. A JSON-RPC error keeps its code and data, which the
// error's own string drops.
func describeDebugError(ctx context.Context, err error) (string, DebugErrorKind) {
	var rpcErr *jsonrpc.Error
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		return "server didn't answer in time: " + err.Error(), DebugTimeout
	case errors.Is(err, ErrNotConnected):
		return err.Error(), DebugNotConnected
	case errors.As(err, &rpcErr):
		msg := fmt.Sprintf("JSON-RPC error %d: %s", rpcErr.Code, rpcErr.Message)
		if len(rpcErr.Data) > 0 {
			msg += "\ndata: " + string(rpcErr.Data)
		}
		return msg + "\n\n" + err.Error(), DebugProtocol
	case errors.As(err, &syntaxErr), errors.As(err, &typeErr),
		strings.Contains(err.Error(), "structuredContent"),
		strings.Contains(err.Error(), "unmarshal"):
		return err.Error(), DebugInvalidResponse
	default:
		return err.Error(), DebugTransport
	}
}
