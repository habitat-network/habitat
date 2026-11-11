// Package mockmcp serves stand-ins for the MCP servers of Google Drive, Slack
// and GitHub, answering from a corpus.Slice instead of a real account, so
// pear's federated search, tool suggestion and tool proxying can be run
// against data whose right answers are known.
//
// Each mock offers its real server's search tool under the same name, taking
// the same arguments and answering in the same shape, since that's what
// internal/mcpsearch's providers call and parse; a tool to read one document
// in full; and the names and descriptions of some of the real server's other
// tools, which do nothing here but are what tool suggestion has to choose
// among. Every call is recorded in a Log, so a run can be scored on what the
// servers were asked rather than on what an agent says it did.
package mockmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/corpus"
)

// App names one of the mocked apps.
type App string

const (
	AppGoogleDrive App = "google-drive"
	AppSlack       App = "slack"
	AppGitHub      App = "github"
)

// Apps are the apps that can be mocked.
var Apps = []App{AppGoogleDrive, AppSlack, AppGitHub}

// Source is the part of the dataset app's mock serves.
func (a App) Source() corpus.Source {
	switch a {
	case AppGoogleDrive:
		return corpus.SourceGoogleDrive
	case AppSlack:
		return corpus.SourceSlack
	default:
		return corpus.SourceGitHub
	}
}

// AppOf is the app serving documents from source.
func AppOf(source corpus.Source) App {
	for _, app := range Apps {
		if app.Source() == source {
			return app
		}
	}
	return ""
}

// SearchTool is the tool of app's server that pear's federated search calls
// for the documents the dataset holds, and so the tool an agent should be
// pointed to for a question answered there.
func (a App) SearchTool() string {
	switch a {
	case AppGoogleDrive:
		return driveSearchTool
	case AppSlack:
		return slackSearchTool
	default:
		return githubSearchTool
	}
}

// Call is one tool call a mock received.
type Call struct {
	App  App            `json:"app"`
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
	// Docs are the documents the call returned, in order.
	Docs []corpus.DocID `json:"docs"`
}

// Log records the calls mocks receive.
type Log struct {
	mu    sync.Mutex
	calls []Call
}

func (l *Log) add(c Call) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, c)
}

// Take returns the calls recorded since the last Take, and forgets them.
func (l *Log) Take() []Call {
	l.mu.Lock()
	defer l.mu.Unlock()
	calls := l.calls
	l.calls = nil
	return calls
}

// Options configure the mocks.
type Options struct {
	// Match is how the mocks' search decides what a query matches; see
	// corpus.Match.
	Match corpus.Match
}

// Server is one app's mock MCP server.
type Server struct {
	App App
	mcp *mcp.Server
}

// New returns app's mock, serving slice's documents from that app and
// recording calls in log.
func New(app App, slice *corpus.Slice, log *Log, opts Options) *Server {
	if opts.Match == "" {
		opts.Match = corpus.MatchAny
	}
	b := &backend{
		app: app, slice: slice, log: log, match: opts.Match,
		index: corpus.NewIndex(slice.OfSource(app.Source())),
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "mock-" + string(app), Version: "0.1.0"}, nil)
	switch app {
	case AppGoogleDrive:
		addDriveTools(srv, b)
	case AppSlack:
		addSlackTools(srv, b)
	case AppGitHub:
		addGitHubTools(srv, b)
	}
	return &Server{App: app, mcp: srv}
}

// Handler serves the mock over streamable HTTP.
func (s *Server) Handler() http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return s.mcp },
		&mcp.StreamableHTTPOptions{DisableLocalhostProtection: true},
	)
}

// backend is what a mock's tools answer from.
type backend struct {
	app   App
	slice *corpus.Slice
	index *corpus.Index
	log   *Log
	match corpus.Match
}

// search runs query over the app's documents and records the call.
func (b *backend) search(
	tool string, args any, query string, limit int, keep func(*corpus.Doc) bool,
) []*corpus.Doc {
	hits := b.index.Search(query, b.match, limit, keep)
	docs := make([]*corpus.Doc, len(hits))
	for i, hit := range hits {
		docs[i] = hit.Doc
	}
	b.record(tool, args, docs...)
	return docs
}

func (b *backend) record(tool string, args any, docs ...*corpus.Doc) {
	call := Call{App: b.app, Tool: tool, Docs: make([]corpus.DocID, len(docs))}
	for i, doc := range docs {
		call.Docs[i] = doc.ID
	}
	// Arguments are logged as the JSON they arrived as.
	if raw, err := json.Marshal(args); err == nil {
		_ = json.Unmarshal(raw, &call.Args)
	}
	b.log.add(call)
}

// find returns the app's document matching ok, or nil.
func (b *backend) find(ok func(*corpus.Doc) bool) *corpus.Doc {
	docs := b.slice.OfSource(b.app.Source())
	if i := slices.IndexFunc(docs, ok); i >= 0 {
		return docs[i]
	}
	return nil
}

// limitOr returns n if it's a usable result limit, else fallback.
func limitOr(n, fallback int) int {
	if n <= 0 || n > 100 {
		return fallback
	}
	return n
}

// snippet returns at most n runes of s.
func snippet(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func errorResult(format string, args ...any) *mcp.CallToolResult {
	res := textResult(fmt.Sprintf(format, args...))
	res.IsError = true
	return res
}

// jsonResult answers with v as JSON text, the way servers without structured
// output do.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return textResult(string(raw)), nil
}

// otherTool is a tool of the real server that the mock offers by name and
// description only. Tool suggestion has to pick the right tool from among
// them, as it would against the real server.
type otherTool struct {
	name, description string
}

// anyArgs is the input of tools whose arguments the mock doesn't read.
type anyArgs map[string]any

func addOtherTools(srv *mcp.Server, b *backend, tools []otherTool) {
	for _, tool := range tools {
		mcp.AddTool(srv, &mcp.Tool{Name: tool.name, Description: tool.description}, func(
			_ context.Context, _ *mcp.CallToolRequest, args anyArgs,
		) (*mcp.CallToolResult, any, error) {
			b.record(tool.name, args)
			return errorResult("%s isn't available in this eval", tool.name), nil, nil
		})
	}
}
