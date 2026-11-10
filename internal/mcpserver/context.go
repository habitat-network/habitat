package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/mcpsearch"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// FederatedSearcher searches a member's connected MCP servers. See
// mcpsearch.Searcher for the concrete implementation.
type FederatedSearcher interface {
	Search(
		ctx context.Context,
		caller syntax.DID,
		org syntax.DID,
		serverURLs []string,
		q mcpsearch.Query,
	) (*mcpsearch.Result, error)
}

const (
	defaultSuggestedTools = 5
	maxSuggestedTools     = 20
	// contextRecordLimit and contextItemLimit are how many records and
	// connected-app results get_context returns from each source.
	contextRecordLimit = 8
	contextItemLimit   = 5
)

const getContextDescription = "Start here. Give it what you're trying to find out or do, in a " +
	"sentence or a few keywords, and it gathers context from everywhere this user's " +
	"organization keeps data: Habitat records and files and the org's connected apps. It " +
	"returns matching records (read one in full with get_record or get_blob), results from " +
	"connected apps, and suggested tools in those apps for the request, each with its input " +
	"schema, to run with call_tool. Call it again when the topic changes."

const callToolDescription = "Run a tool of one of the organization's connected apps, by the " +
	"name get_context suggested (for example \"github:search_issues\"), with arguments " +
	"matching that tool's input_schema."

type getContextInput struct {
	Query string `json:"query" jsonschema:"what you want to find out or do, in a sentence or a few keywords"`
	// MaxTools is a pointer so omitted, which takes the default, differs
	// from an explicit zero, which asks for no suggestions.
	MaxTools *int `json:"max_tools,omitempty" jsonschema:"most tools to suggest, 0 to 20; defaults to 5"`
}

type contextRecord struct {
	URI     string `json:"uri"`
	Space   string `json:"space"`
	Snippet string `json:"snippet"`
	Value   any    `json:"value"`
}

type contextItem struct {
	Server  string `json:"server"`
	Title   string `json:"title"`
	URL     string `json:"url,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

type suggestedTool struct {
	// Name is what to pass to call_tool.
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	InputSchema any     `json:"input_schema,omitempty"`
	Score       float64 `json:"score" jsonschema:"how well the tool fits the query, 0 to 1"`
}

type getContextOutput struct {
	Records        []contextRecord `json:"records"`
	Items          []contextItem   `json:"items"`
	SuggestedTools []suggestedTool `json:"suggested_tools"`
	// Notes say which sources couldn't be searched and why, so an empty
	// section isn't mistaken for "nothing there".
	Notes []string `json:"notes,omitempty"`
}

// contextAgent gathers what get_context returns for a query: it runs the
// record search, the federated search over connected apps, and tool
// selection concurrently, each as the caller. A source that fails is
// reported in the output's notes rather than failing the whole call.
type contextAgent struct {
	org              syntax.DID
	records          RecordSearcher
	federated        FederatedSearcher
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
		return nil, a.gather(ctx, syntax.DID(tokenInfo.UserID), input.Query, maxTools), nil
	}
}

func (a *contextAgent) gather(
	ctx context.Context,
	caller syntax.DID,
	query string,
	maxTools int,
) getContextOutput {
	out := getContextOutput{
		Records: []contextRecord{}, Items: []contextItem{}, SuggestedTools: []suggestedTool{},
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		note = func(format string, args ...any) {
			mu.Lock()
			defer mu.Unlock()
			out.Notes = append(out.Notes, fmt.Sprintf(format, args...))
		}
	)
	if a.records != nil {
		wg.Go(func() {
			page, err := a.records.Search(ctx, caller, search.Query{
				Text: query, Org: a.org, Limit: contextRecordLimit,
				SnippetLength: defaultMCPSnippetLength,
			})
			if err != nil {
				slog.WarnContext(ctx, "mcp: get_context record search", "err", err)
				note("Habitat record search failed")
				return
			}
			for _, m := range page.Matches {
				out.Records = append(out.Records, contextRecord{
					URI: m.URI.String(), Space: m.Record.Space.String(),
					Snippet: m.Snippet, Value: m.Record.Value,
				})
			}
		})
	}
	if a.federated != nil {
		wg.Go(func() {
			res, err := a.federated.Search(ctx, caller, a.org, nil, mcpsearch.Query{
				Text: query, Limit: contextItemLimit,
			})
			if err != nil {
				slog.WarnContext(ctx, "mcp: get_context federated search", "err", err)
				note("searching connected apps failed")
				return
			}
			for _, item := range res.Items {
				out.Items = append(out.Items, contextItem{
					Server: string(item.Source.ServerName), Title: item.Title,
					URL: item.URL, Snippet: item.Snippet,
				})
			}
			for _, s := range res.Sources {
				switch s.Status {
				case mcpsearch.StatusOK, mcpsearch.StatusUnsupported:
				case mcpsearch.StatusNotConnected:
					note("%s: not connected", s.ServerName)
				default:
					note("%s: search %s", s.ServerName, s.Status)
				}
			}
		})
	}
	if a.connectedServers != nil && maxTools > 0 {
		wg.Go(func() {
			tools, err := a.selectTools(ctx, caller, query, maxTools)
			if err != nil {
				slog.WarnContext(ctx, "mcp: get_context tool selection", "err", err)
				note("suggesting tools failed")
				return
			}
			out.SuggestedTools = tools
		})
	}
	wg.Wait()
	return out
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
