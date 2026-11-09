// Package toolsearch is a searchable catalog of the tools of the MCP servers
// a member has connected (see internal/mcpgateway), for progressive tool
// discovery: instead of listing every tool up front, a host lets the model
// search for the few it needs and load only their definitions. See
// https://modelcontextprotocol.io/docs/2026-07-28/develop/clients/client-best-practices.
//
// The catalog is a Meilisearch index of one document per (org, member,
// server, tool). It is per member because what a server lists can depend on
// who is asking. Searches are keyword, or hybrid with semantic matches when
// an embedder is configured, like internal/search.
package toolsearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/meilisearch/meilisearch-go"

	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/search"
)

// ErrNotFound is returned by Get for a tool the catalog doesn't have.
var ErrNotFound = errors.New("tool not found")

const (
	// taskPollInterval is how often Meilisearch is polled while waiting for
	// a write to be applied.
	taskPollInterval = 20 * time.Millisecond
	// embedderName is the Meilisearch embedder tools are embedded with.
	embedderName = "default"
	// DefaultLimit is how many tools Search returns when not told.
	DefaultLimit = 10
	// MaxLimit is the most tools Search returns.
	MaxLimit = 50
)

// Tool is a tool of a connected MCP server.
type Tool struct {
	// Server is the org-chosen name of the server the tool belongs to.
	Server string
	// Name is the tool's name as the catalog's callers know it.
	Name string
	// Description is the tool's one-line summary, searched and returned by
	// Search.
	Description string
	// Definition is the tool's complete MCP definition as JSON, with its
	// input schema, which only Get returns.
	Definition json.RawMessage
}

// Hit is a search result: a tool without its definition.
type Hit struct {
	Server      string
	Name        string
	Description string
}

// doc is a tool as stored in Meilisearch.
type doc struct {
	ID          string          `json:"id"`
	Org         string          `json:"org"`
	Member      string          `json:"member"`
	Server      string          `json:"server"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Definition  json.RawMessage `json:"definition,omitempty"`
	// SyncedAt is when the document was written, in unix microseconds (finer
	// would lose precision as a Meilisearch number), so a
	// server's replacement can delete what it didn't rewrite.
	SyncedAt int64 `json:"synced_at"`
}

// Index is a catalog stored in a Meilisearch index.
type Index struct {
	index    meilisearch.IndexManager
	client   meilisearch.ServiceManager
	embedder *search.Embedder
}

// Option configures an Index.
type Option func(*Index)

// WithEmbedder makes searches hybrid, combining keyword matches with
// semantic matches from embeddings e computes, so "send a message" finds a
// tool described as "post to a channel". Without it, any embedder configured
// on the index earlier is removed and searches are keyword only.
func WithEmbedder(e search.Embedder) Option {
	return func(i *Index) { i.embedder = &e }
}

var (
	searchableAttributes = []string{"name", "description", "server"}
	filterableAttributes = []string{"org", "member", "server", "synced_at"}
)

// Open returns the catalog stored in the Meilisearch index uid on the server
// at host, creating and configuring the index if needed. The catalog is
// rebuilt lazily from the servers themselves, so changing its settings or
// embedder needs no migration.
func Open(
	ctx context.Context,
	host, apiKey, uid string,
	opts ...Option,
) (*Index, error) {
	return open(ctx, meilisearch.New(
		host,
		meilisearch.WithAPIKey(apiKey),
		meilisearch.WithCustomClient(httpx.NewClient()),
	), uid, opts...)
}

func open(
	ctx context.Context,
	client meilisearch.ServiceManager,
	uid string,
	opts ...Option,
) (*Index, error) {
	i := &Index{client: client, index: client.Index(uid)}
	for _, opt := range opts {
		opt(i)
	}
	info, err := client.CreateIndexWithContext(ctx, &meilisearch.IndexConfig{
		Uid: uid, PrimaryKey: "id",
	})
	if err != nil {
		return nil, fmt.Errorf("create meilisearch index %s: %w", uid, err)
	}
	// An index that already exists makes the task fail, which is fine.
	if err := i.wait(ctx, info); err != nil && !strings.Contains(err.Error(), "index_already_exists") {
		return nil, fmt.Errorf("create meilisearch index %s: %w", uid, err)
	}
	settings := &meilisearch.Settings{
		SearchableAttributes: searchableAttributes,
		FilterableAttributes: filterableAttributes,
	}
	if i.embedder != nil {
		settings.Embedders = map[string]meilisearch.Embedder{
			// Never renders empty, which embeddings APIs reject.
			embedderName: i.embedder.Settings(
				`{{doc.server}} {{doc.name}}{% if doc.description != "" %}: {{doc.description}}{% endif %}`,
			),
		}
	}
	info, err = i.index.UpdateSettingsWithContext(ctx, settings)
	if err != nil {
		return nil, fmt.Errorf("configure meilisearch index %s: %w", uid, err)
	}
	if err := i.wait(ctx, info); err != nil {
		return nil, fmt.Errorf("configure meilisearch index %s: %w", uid, err)
	}
	if i.embedder == nil {
		// Embedders are omitted from settings when empty, so drop one left
		// from an earlier configuration explicitly.
		info, err = i.index.ResetEmbeddersWithContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("reset meilisearch embedders: %w", err)
		}
		if err := i.wait(ctx, info); err != nil {
			return nil, fmt.Errorf("reset meilisearch embedders: %w", err)
		}
	}
	return i, nil
}

func (i *Index) wait(ctx context.Context, info *meilisearch.TaskInfo) error {
	task, err := i.client.WaitForTaskWithContext(ctx, info.TaskUID, taskPollInterval)
	if err != nil {
		return fmt.Errorf("wait for meilisearch task: %w", err)
	}
	if task.Status != meilisearch.TaskStatusSucceeded {
		return fmt.Errorf("meilisearch task failed: %s: %s", task.Error.Code, task.Error.Message)
	}
	return nil
}

// docID identifies a tool of a member's server.
func docID(org, member syntax.DID, server, name string) string {
	h := sha256.Sum256([]byte(strings.Join([]string{
		org.String(), member.String(), server, name,
	}, "\x00")))
	return hex.EncodeToString(h[:16])
}

// ReplaceServer makes tools the catalog's tools of server for member in org,
// replacing the ones it had. The new tools are written before the old are
// deleted, so a concurrent search never finds the server empty.
func (i *Index) ReplaceServer(
	ctx context.Context,
	org, member syntax.DID,
	server string,
	tools []Tool,
) error {
	now := time.Now().UnixMicro()
	if len(tools) > 0 {
		docs := make([]doc, len(tools))
		for n, t := range tools {
			docs[n] = doc{
				ID:          docID(org, member, server, t.Name),
				Org:         org.String(),
				Member:      member.String(),
				Server:      server,
				Name:        t.Name,
				Description: t.Description,
				Definition:  t.Definition,
				SyncedAt:    now,
			}
		}
		info, err := i.index.AddDocumentsWithContext(ctx, docs, nil)
		if err != nil {
			return fmt.Errorf("put tools: %w", err)
		}
		if err := i.wait(ctx, info); err != nil {
			return err
		}
	}
	return i.deleteWhere(ctx, fmt.Sprintf("%s AND synced_at < %d",
		serverFilter(org, member, server), now))
}

// PruneServers deletes member's tools in org from every server not named in
// keep, such as ones since disconnected.
func (i *Index) PruneServers(ctx context.Context, org, member syntax.DID, keep []string) error {
	filter := memberFilter(org, member)
	if len(keep) > 0 {
		quoted := make([]string, len(keep))
		for n, k := range keep {
			quoted[n] = quote(k)
		}
		filter += " AND server NOT IN [" + strings.Join(quoted, ", ") + "]"
	}
	return i.deleteWhere(ctx, filter)
}

func (i *Index) deleteWhere(ctx context.Context, filter string) error {
	info, err := i.index.DeleteDocumentsByFilterWithContext(ctx, filter, nil)
	if err != nil {
		return fmt.Errorf("delete tools: %w", err)
	}
	return i.wait(ctx, info)
}

// Search returns up to limit of member's tools in org that best match query,
// best first. Only that member's tools in that org are ever searched.
func (i *Index) Search(
	ctx context.Context,
	org, member syntax.DID,
	query string,
	limit int,
) ([]Hit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	req := &meilisearch.SearchRequest{
		Limit:                int64(limit),
		Filter:               memberFilter(org, member),
		AttributesToRetrieve: []string{"server", "name", "description"},
	}
	if i.embedder != nil {
		req.Hybrid = &meilisearch.SearchRequestHybrid{
			Embedder:      embedderName,
			SemanticRatio: i.embedder.SemanticRatio,
		}
		req.RankingScoreThreshold = i.embedder.MinScore
	}
	resp, err := i.index.SearchWithContext(ctx, query, req)
	if err != nil {
		return nil, fmt.Errorf("search tools: %w", err)
	}
	var docs []doc
	if err := resp.Hits.DecodeInto(&docs); err != nil {
		return nil, fmt.Errorf("decode tools: %w", err)
	}
	hits := make([]Hit, len(docs))
	for n, d := range docs {
		hits[n] = Hit{Server: d.Server, Name: d.Name, Description: d.Description}
	}
	return hits, nil
}

// Get returns member's tool name of server in org with its definition, or
// ErrNotFound.
func (i *Index) Get(
	ctx context.Context,
	org, member syntax.DID,
	server, name string,
) (*Tool, error) {
	var d doc
	err := i.index.GetDocumentWithContext(ctx, docID(org, member, server, name), nil, &d)
	var apiErr *meilisearch.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tool: %w", err)
	}
	return &Tool{
		Server: d.Server, Name: d.Name, Description: d.Description, Definition: d.Definition,
	}, nil
}

func memberFilter(org, member syntax.DID) string {
	return "org = " + quote(org.String()) + " AND member = " + quote(member.String())
}

func serverFilter(org, member syntax.DID, server string) string {
	return memberFilter(org, member) + " AND server = " + quote(server)
}

// quote quotes v as a Meilisearch filter value.
func quote(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}
