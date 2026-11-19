package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/fedsearch"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultSuggestedTools = 5
	maxSuggestedTools     = 20
	// contextPageSize is how many results one get_context page holds.
	contextPageSize = 10
	// contextTimeout is how long get_context waits for search backends
	// before returning what's ready. A backend still running keeps going,
	// and next_cursor waits for it.
	contextTimeout = 10 * time.Second
)

const getContextDescription = "Start here. Give it what you're trying to find out or do, in a " +
	"sentence or a few keywords, and it gathers context from everywhere this user's " +
	"organization keeps data: Habitat records and files and the org's connected apps. It " +
	"returns matching results from all of them, ranked together (read a record in full with " +
	"get_record or get_blob), and suggested tools in those apps for the request, each with " +
	"its input schema, to run with call_tool. Sources that are slow or failed are listed in " +
	"backends; while one is pending, next_cursor waits for its results. Call it again when " +
	"the topic changes."

const callToolDescription = "Run a tool of one of the organization's connected apps, by the " +
	"name get_context suggested (for example \"github:search_issues\"), with arguments " +
	"matching that tool's input_schema."

type getContextInput struct {
	Query string `json:"query,omitempty" jsonschema:"what you want to find out or do, in a sentence or a few keywords; required unless cursor is given"`
	// MaxTools is a pointer so omitted, which takes the default, differs
	// from an explicit zero, which asks for no suggestions.
	MaxTools *int `json:"max_tools,omitempty" jsonschema:"most tools to suggest, 0 to 20; defaults to 5"`
	// Cursor continues a previous search: it returns the next page of that
	// search's results. See the MCP pagination utility.
	Cursor *string `json:"cursor,omitempty" jsonschema:"next_cursor from a previous get_context response, to get its next page of results; omit to start a search"`
}

// contextResult is one search result, from any source.
type contextResult struct {
	// Source is the backend the result came from, such as "records" or
	// "mcp:slack/slack-messages".
	Source  string `json:"source"`
	Kind    string `json:"kind"`
	Title   string `json:"title,omitempty"`
	URL     string `json:"url,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	// Space and Value are set for a Habitat record, whose URL is its URI.
	Space string         `json:"space,omitempty"`
	Value map[string]any `json:"value,omitempty"`
}

// contextBackend is a source that hasn't answered yet or couldn't be
// searched.
type contextBackend struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type suggestedTool struct {
	// Name is what to pass to call_tool.
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	InputSchema any     `json:"input_schema,omitempty"`
	Score       float64 `json:"score"                  jsonschema:"fit to the query, 0 to 1"`
}

type getContextOutput struct {
	Results        []contextResult `json:"results"`
	SuggestedTools []suggestedTool `json:"suggested_tools"`
	// Backends lists the sources that are still pending or failed, so an
	// empty result isn't mistaken for "nothing there".
	Backends []contextBackend `json:"backends,omitempty"`
	// Notes say what else went wrong, such as tool suggestion failing.
	Notes []string `json:"notes,omitempty"`
	// NextCursor is present while more results are ranked beyond this page
	// or a source is still pending. A missing one means the end of results.
	NextCursor *string `json:"next_cursor,omitempty" jsonschema:"pass as cursor to get the next page of results"`
}

// contextAgent gathers what get_context returns for a query: it runs the
// federated search (see internal/fedsearch) and tool selection
// concurrently, each as the caller. A source that fails is reported in the
// output rather than failing the whole call.
type contextAgent struct {
	org syntax.DID
	// search may be nil, when nothing is searched.
	search           *fedsearch.Service
	connectedServers mcpgateway.ConnectedServers
	selector         ToolSelector
}

func (a *contextAgent) handler() mcp.ToolHandlerFor[getContextInput, getContextOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input getContextInput,
	) (*mcp.CallToolResult, getContextOutput, error) {
		tokenInfo := auth.TokenInfoFromContext(ctx)
		if tokenInfo == nil || tokenInfo.UserID == "" {
			return nil, getContextOutput{}, fmt.Errorf("missing authenticated caller")
		}
		caller := syntax.DID(tokenInfo.UserID)
		if input.Cursor != nil {
			out, err := a.page(ctx, caller, *input.Cursor)
			return nil, out, err
		}
		if input.Query == "" {
			return nil, getContextOutput{}, fmt.Errorf("query is required")
		}
		maxTools := defaultSuggestedTools
		if input.MaxTools != nil {
			maxTools = *input.MaxTools
		}
		if maxTools < 0 || maxTools > maxSuggestedTools {
			return nil, getContextOutput{}, fmt.Errorf(
				"max_tools must be between 0 and %d", maxSuggestedTools,
			)
		}
		out, err := a.gather(ctx, caller, input.Query, maxTools)
		return nil, out, err
	}
}

// errInvalidCursor is get_context's answer to an unknown, expired or someone
// else's cursor: Invalid params, as the MCP pagination utility asks.
var errInvalidCursor = &jsonrpc.Error{
	Code:    jsonrpc.CodeInvalidParams,
	Message: "invalid or expired cursor; call get_context again with a query",
}

// page returns the page of a previous search that cursor points at, waiting
// for a pending source as long as contextTimeout.
func (a *contextAgent) page(
	ctx context.Context,
	caller syntax.DID,
	cursor string,
) (getContextOutput, error) {
	if a.search == nil {
		return getContextOutput{}, errInvalidCursor
	}
	page, err := a.search.Continue(ctx, caller, cursor, contextPageSize, contextTimeout)
	if errors.Is(err, fedsearch.ErrInvalidCursor) || errors.Is(err, fedsearch.ErrNotFound) {
		return getContextOutput{}, errInvalidCursor
	}
	if err != nil {
		return getContextOutput{}, err
	}
	out := getContextOutput{SuggestedTools: []suggestedTool{}}
	fillFromPage(ctx, &out, page)
	return out, nil
}

func (a *contextAgent) gather(
	ctx context.Context,
	caller syntax.DID,
	query string,
	maxTools int,
) (getContextOutput, error) {
	out := getContextOutput{Results: []contextResult{}, SuggestedTools: []suggestedTool{}}
	var (
		wg      sync.WaitGroup
		page    fedsearch.Page
		pageErr error
	)
	if a.search != nil {
		wg.Go(func() {
			s, err := a.search.Start(ctx, caller, a.org, fedsearch.Query{Text: query})
			if err != nil {
				pageErr = err
				return
			}
			page, pageErr = s.Next(ctx, fedsearch.NextOptions{
				Limit: contextPageSize, Timeout: contextTimeout, WaitAll: true,
			})
		})
	}
	if a.connectedServers != nil && maxTools > 0 {
		wg.Go(func() {
			tools, err := a.selectTools(ctx, caller, query, maxTools)
			if err != nil {
				slog.WarnContext(ctx, "mcp: get_context tool selection", "err", err)
				out.Notes = append(out.Notes, "suggesting tools failed")
				return
			}
			out.SuggestedTools = tools
		})
	}
	wg.Wait()
	if pageErr != nil {
		return getContextOutput{}, fmt.Errorf("search: %w", pageErr)
	}
	fillFromPage(ctx, &out, page)
	return out, nil
}

// fillFromPage sets out's results, backends and next cursor from page.
func fillFromPage(ctx context.Context, out *getContextOutput, page fedsearch.Page) {
	for _, b := range page.Backends {
		if b.Status == fedsearch.StatusOK {
			continue
		}
		cb := contextBackend{Name: b.Name, Status: string(b.Status)}
		if b.Error != nil {
			cb.Error = b.Error.Message
		}
		out.Backends = append(out.Backends, cb)
	}
	out.Results = make([]contextResult, len(page.Results))
	for i, r := range page.Results {
		out.Results[i] = contextResult{
			Source: string(r.Backend), Kind: string(r.Kind),
			Title: r.Title, URL: r.URL, Snippet: r.Snippet,
		}
		if r.Kind != search.ResultKind {
			continue
		}
		var record search.RecordData
		if err := json.Unmarshal(r.Data, &record); err != nil {
			slog.WarnContext(ctx, "mcp: get_context record result", "id", r.ID, "err", err)
			continue
		}
		out.Results[i].Space, out.Results[i].Value = record.Space.String(), record.Value
	}
	if page.Cursor != "" {
		out.NextCursor = &page.Cursor
	}
}

// selectTools lists caller's connected servers' tools and asks the selector
// for those that fit query.
func (a *contextAgent) selectTools(
	ctx context.Context,
	caller syntax.DID,
	query string,
	limit int,
) ([]suggestedTool, error) {
	servers := listConnectedServers(ctx, a.connectedServers, caller, a.org)
	var candidates []ToolCandidate
	byName := make(map[string]*mcp.Tool)
	for i, tools := range listToolsForServers(ctx, a.connectedServers, caller, servers) {
		for _, tool := range tools {
			name := namespaceTool(string(servers[i].Name), tool.Name)
			candidates = append(candidates, ToolCandidate{
				Name: name, Description: tool.Description,
			})
			byName[name] = tool
		}
	}
	scored, err := a.selector.Select(ctx, query, candidates, limit)
	if err != nil {
		return nil, err
	}
	out := make([]suggestedTool, len(scored))
	for i, s := range scored {
		out[i] = suggestedTool{
			Name: s.Name, Description: s.Description,
			InputSchema: byName[s.Name].InputSchema, Score: s.Score,
		}
	}
	return out, nil
}

type callToolInput struct {
	Name      string         `json:"name"                jsonschema:"a tool name from get_context's suggested_tools, like github:search_issues"`
	Arguments map[string]any `json:"arguments,omitempty" jsonschema:"the tool's arguments, matching its input_schema"`
}

// callToolHandler implements "call_tool": it runs a connected server's tool
// as the caller and returns that server's result unchanged. The tool must
// belong to a server the caller is connected to in org.
func callToolHandler(
	connectedServers mcpgateway.ConnectedServers,
	org syntax.DID,
) mcp.ToolHandlerFor[callToolInput, any] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input callToolInput,
	) (*mcp.CallToolResult, any, error) {
		tokenInfo := auth.TokenInfoFromContext(ctx)
		if tokenInfo == nil || tokenInfo.UserID == "" {
			return nil, nil, fmt.Errorf("missing authenticated caller")
		}
		caller := syntax.DID(tokenInfo.UserID)
		serverName, toolName, ok := splitNamespacedTool(input.Name)
		if !ok {
			return nil, nil, fmt.Errorf(
				"tool name %q must look like server:tool, as in get_context's suggestions",
				input.Name,
			)
		}
		for _, server := range listConnectedServers(ctx, connectedServers, caller, org) {
			if string(server.Name) != serverName {
				continue
			}
			res, err := callConnectedTool(
				ctx, connectedServers, caller, server, toolName, input.Arguments,
			)
			if err != nil {
				return nil, nil, err
			}
			// Hand back the downstream result as is, including its error flag.
			return res.(*mcp.CallToolResult), nil, nil
		}
		return nil, nil, fmt.Errorf("no connected app %q", serverName)
	}
}
