package mcpsearch

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ProviderID identifies a Provider, e.g. "github-issues". It's reported with
// every item and source so callers can tell which tool produced them.
type ProviderID string

// Provider adapts one known search tool of one kind of MCP server: it says
// which servers have the tool, builds the tool's arguments from a Query, and
// normalizes its result into Items. A server can have several providers (for
// example one per search tool it offers), each called in parallel.
//
// Most providers are a ToolProvider literal; implement Provider directly only
// for a tool whose result needs custom parsing.
type Provider interface {
	// ID is the provider's unique, stable identifier.
	ID() ProviderID
	// Matches reports whether the MCP server at serverURL, its configured
	// streamable HTTP endpoint (see CanonicalURL), offers this provider's
	// tool.
	Matches(serverURL *url.URL) bool
	// ToolName is the name of the tool to call on the server.
	ToolName() string
	// Arguments builds the tool call's arguments for q.
	Arguments(q Query) map[string]any
	// Normalize converts a successful tool result into items, in the order
	// the tool ranked them. Their Source is filled in by the caller.
	Normalize(result *mcp.CallToolResult) ([]Item, error)
}

// ErrUnparseableResult is returned by ToolProvider.Normalize when a tool
// result has neither structured content nor JSON text to read items from.
var ErrUnparseableResult = errors.New("search tool result isn't JSON")

// ToolProvider is a Provider for the common case: a tool whose result is a
// JSON object (as structured content, or as the text of its first text
// content block) holding a list of results at ItemsPath, each with its title,
// URL and snippet at fixed paths.
type ToolProvider struct {
	// Name is the provider's ID.
	Name ProviderID
	// URL is the server's streamable HTTP endpoint. A server matches if its
	// URL is the same, compared with CanonicalURL.
	URL string
	// Tool is the tool to call.
	Tool string
	// Args builds the tool's arguments from a query.
	Args func(q Query) map[string]any
	// ItemsPath is the dot-separated path to the result's list of items, e.g.
	// "items". Empty means the result is itself the list.
	ItemsPath string
	// TitlePaths, URLPaths and SnippetPaths are dot-separated paths within
	// each item; the first one that holds a non-empty string is used.
	TitlePaths   []string
	URLPaths     []string
	SnippetPaths []string
	// TextFallback, if set, returns the tool's text content as a single
	// snippet-only item when it isn't JSON, for tools that answer in prose
	// or markdown, instead of failing with ErrUnparseableResult.
	TextFallback bool
}

var _ Provider = (*ToolProvider)(nil)

func (p *ToolProvider) ID() ProviderID { return p.Name }

func (p *ToolProvider) Matches(serverURL *url.URL) bool {
	want, err := CanonicalURL(p.URL)
	return err == nil && want == serverURL.String()
}

func (p *ToolProvider) ToolName() string { return p.Tool }

func (p *ToolProvider) Arguments(q Query) map[string]any { return p.Args(q) }

func (p *ToolProvider) Normalize(result *mcp.CallToolResult) ([]Item, error) {
	text := resultText(result)
	var doc any
	switch {
	case result.StructuredContent != nil:
		// StructuredContent decoded by the client is a plain JSON value
		// (map[string]any etc.), but round-trip it so a provider never depends
		// on how it was produced.
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return nil, fmt.Errorf("encode structured content: %w", err)
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("decode structured content: %w", err)
		}
	case json.Unmarshal([]byte(text), &doc) == nil:
	case p.TextFallback && strings.TrimSpace(text) != "":
		return []Item{{Snippet: text}}, nil
	case p.TextFallback:
		return nil, nil
	default:
		return nil, ErrUnparseableResult
	}

	list, ok := lookup(doc, p.ItemsPath).([]any)
	if !ok {
		// No list (for example an empty result that omits it) means no
		// matches rather than an error: tools commonly drop empty arrays.
		return nil, nil
	}
	items := make([]Item, 0, len(list))
	for _, entry := range list {
		item := Item{
			Title:   firstString(entry, p.TitlePaths),
			URL:     firstString(entry, p.URLPaths),
			Snippet: firstString(entry, p.SnippetPaths),
		}
		if item == (Item{}) {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

// resultText joins the text of result's text content blocks.
func resultText(result *mcp.CallToolResult) string {
	var parts []string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// lookup follows the dot-separated path through nested JSON objects in doc.
// An empty path is doc itself; a path that doesn't exist is nil.
func lookup(doc any, path string) any {
	if path == "" {
		return doc
	}
	for key := range strings.SplitSeq(path, ".") {
		obj, ok := doc.(map[string]any)
		if !ok {
			return nil
		}
		doc = obj[key]
	}
	return doc
}

// firstString returns the first non-empty string found at one of paths in
// doc.
func firstString(doc any, paths []string) string {
	for _, path := range paths {
		if s, ok := lookup(doc, path).(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// CanonicalURL normalizes an MCP server URL so URLs that address the same
// endpoint compare equal: the scheme and host are lowercased, a default port
// and a trailing slash are dropped, and the fragment is removed.
func CanonicalURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	if !u.IsAbs() || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("%q isn't an absolute http(s) url", raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if port := u.Port(); (u.Scheme == "https" && port == "443") ||
		(u.Scheme == "http" && port == "80") {
		u.Host = u.Hostname()
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawPath = ""
	u.Fragment, u.RawFragment = "", ""
	u.User = nil
	return u.String(), nil
}

// Registry holds the known search providers.
type Registry struct {
	providers []Provider
}

// NewRegistry returns a registry of providers, which are called in the given
// order for a server that several of them match. It panics on a duplicate ID,
// which is a programming error.
func NewRegistry(providers ...Provider) *Registry {
	seen := make(map[ProviderID]bool, len(providers))
	for _, p := range providers {
		if seen[p.ID()] {
			panic(fmt.Sprintf("mcpsearch: duplicate provider %q", p.ID()))
		}
		seen[p.ID()] = true
	}
	return &Registry{providers: providers}
}

// ForURL returns the providers whose tools the server at serverURL offers,
// in registration order.
func (r *Registry) ForURL(serverURL string) []Provider {
	canonical, err := CanonicalURL(serverURL)
	if err != nil {
		return nil
	}
	u, err := url.Parse(canonical)
	if err != nil {
		return nil
	}
	var out []Provider
	for _, p := range r.providers {
		if p.Matches(u) {
			out = append(out, p)
		}
	}
	return out
}
