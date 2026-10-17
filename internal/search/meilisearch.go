package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/meilisearch/meilisearch-go"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// ErrInvalidCursor is returned by Search for a cursor it didn't issue.
var ErrInvalidCursor = errors.New("invalid search cursor")

const (
	// taskPollInterval is how often Meilisearch is polled while waiting for
	// a write to be applied.
	taskPollInterval = 20 * time.Millisecond
	// accessPageSize is how many documents SetSpaceAccess reads and rewrites
	// at a time.
	accessPageSize = 1000
	// stateRecheckInterval is how often an index opened with
	// [OpenMeilisearch] rechecks its settings: whether it has the embedder,
	// and whether it predates the current projection.
	stateRecheckInterval = 30 * time.Second
	// projectionVersion versions how records become stored documents. A
	// change to it, or to [indexSettings], needs `pear run reindex`.
	projectionVersion = 2
	// recordSource is the chunk source of a record's own text; a blob's
	// chunks use [blobSource].
	recordSource = "r"
)

// ErrLegacyIndex is returned by writes to an index built before the current
// projection, which a reindex replaces. The indexer retries them, so writes
// resume once the reindex swaps in a current index.
var ErrLegacyIndex = errors.New(
	"search index predates the current projection; run `pear run reindex`")

// Attributes of the fixed document schema. Every collection maps its fields
// onto these, so a change to what a collection indexes rewrites documents,
// never index settings.
var (
	searchableAttributes = []string{"title", "body", "extra"}
	// filterableAttributes are the scope, access and chunk fields, and the
	// typed slots collections map their fields onto.
	filterableAttributes = []string{
		"space_owner", "space_type", "space_key", "repo", "collection",
		"user_readers", "community_role_readers", "public",
		"parent_id", "rev", "projection", "users", "date", "facets",
		"num_0", "num_1", "num_2", "num_3", "date_0", "date_1", "date_2", "date_3",
	}
	sortableAttributes = []string{
		"date", "num_0", "num_1", "num_2", "num_3",
		"date_0", "date_1", "date_2", "date_3", "str_0", "str_1",
	}
)

// meiliDoc is one chunk of a [Document] as stored in Meilisearch.
type meiliDoc struct {
	// ID is <parent>_<source>_<chunk>, since the URI has characters
	// Meilisearch doesn't allow in a document id.
	ID string `json:"id"`
	// ParentID is derived from the URI and shared by all of a record's
	// chunks, which searches deduplicate on.
	ParentID string `json:"parent_id"`
	URI      string `json:"uri"`
	// SpaceOwner, SpaceType and SpaceKey are the parts of the document's
	// space URI, which is not stored whole. Collection scopes are keyed by
	// SpaceOwner.
	SpaceOwner string `json:"space_owner"`
	SpaceType  string `json:"space_type"`
	SpaceKey   string `json:"space_key"`
	Repo       string `json:"repo"`
	Collection string `json:"collection"`
	Rev        string `json:"rev"`
	Projection int    `json:"projection"`
	ConfigRev  string `json:"config_rev"`
	// Source is [recordSource] or a [blobSource], and Chunk the chunk's
	// position in it.
	Source string `json:"source"`
	Chunk  int    `json:"chunk"`
	// Title is always present, since the embedder template reads it. It is
	// on each of the record's chunks, and Extra on its first.
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Extra  string   `json:"extra,omitempty"`
	Facets []string `json:"facets"`
	Users  []string `json:"users"`
	// Date is Unix seconds, absent when the record has none.
	Date *int64 `json:"date,omitempty"`
	// Slots holds the values in each [Slot], datetimes as Unix seconds,
	// stored as top-level fields named by the slot.
	Slots map[Slot]any `json:"-"`
	meiliAccess
}

// MarshalJSON stores d's slots as top-level fields.
func (d meiliDoc) MarshalJSON() ([]byte, error) {
	type plain meiliDoc
	raw, err := json.Marshal(plain(d))
	if err != nil || len(d.Slots) == 0 {
		return raw, err
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	for slot, v := range d.Slots {
		fields[string(slot)] = v
	}
	return json.Marshal(fields)
}

// meiliAccess is an [Access] as filterable document fields. Each principal is
// flattened to one string, so a reader matches a document by sharing one.
type meiliAccess struct {
	UserReaders          []string `json:"user_readers"`
	CommunityRoleReaders []string `json:"community_role_readers"`
	Public               bool     `json:"public"`
}

// meiliAccessUpdate partially updates a document's access fields.
type meiliAccessUpdate struct {
	ID string `json:"id"`
	meiliAccess
}

// meiliHit is a search hit's retrieved fields.
type meiliHit struct {
	URI        string `json:"uri"`
	SpaceOwner string `json:"space_owner"`
	SpaceType  string `json:"space_type"`
	SpaceKey   string `json:"space_key"`
	Repo       string `json:"repo"`
	Collection string `json:"collection"`
	Formatted  struct {
		Body string `json:"body"`
		// Text is the snippet field of an index built before the current
		// projection.
		Text string `json:"text"`
	} `json:"_formatted"`
}

// meiliCursor is a [RepoCursor] as stored in Meilisearch.
type meiliCursor struct {
	ID    string `json:"id"`
	Space string `json:"space"`
	Repo  string `json:"repo"`
	Rev   string `json:"rev"`
}

// Meilisearch is an [Index] stored in two Meilisearch indexes: one of
// documents, and one of repo cursors.
type Meilisearch struct {
	index   meilisearch.IndexManager
	cursors meilisearch.IndexManager
	client  meilisearch.ServiceManager
	// embedder, when set, makes searches hybrid; see [WithEmbedder].
	embedder *Embedder
	// recheck, when set, means the index's settings may differ from the
	// ones opts describe, and change when a reindex swaps in a new index, so
	// they are read back periodically. Without it they are the ones opts
	// describe.
	recheck *stateCheck
}

// indexState is what the index's settings say about how to use it.
type indexState struct {
	// hasEmbedder is whether the index has the embedder.
	hasEmbedder bool
	// legacy is whether the index predates the current projection: it
	// searches one text field and its documents aren't chunks.
	legacy bool
}

// stateCheck caches an index's state.
type stateCheck struct {
	mu    sync.Mutex
	state indexState
	at    time.Time
}

var _ Index = (*Meilisearch)(nil)

// Embedder is an OpenAI-compatible embeddings API that Meilisearch calls
// itself to embed each document's text and each query, for hybrid search.
// Any server implementing POST /v1/embeddings works, such as Ollama
// (http://localhost:11434/v1/embeddings) or Microsoft Foundry
// (https://<resource>.openai.azure.com/openai/v1/embeddings).
type Embedder struct {
	// URL is the full embeddings endpoint, including the /embeddings path.
	URL string
	// APIKey, when set, is sent as a bearer token.
	APIKey string
	// Model is sent as the request's model; for Foundry, the deployment name.
	Model string
	// Dimensions is the embeddings' size. Zero lets Meilisearch infer it by
	// embedding a test string.
	Dimensions int
	// SemanticRatio weighs semantic against keyword results, from 0 (keyword
	// only) to 1 (semantic only).
	SemanticRatio float64
	// MinScore drops hits whose ranking score, from 0 to 1, is below it. A
	// semantic search ranks every document by similarity, so without it a
	// query also returns every unrelated document it may read, last. A good
	// value depends on the model. Zero keeps every hit.
	MinScore float64
}

const (
	// embedderName is the Meilisearch embedder documents are embedded with.
	embedderName = "default"
	// embedTemplateMaxBytes caps how much of a document's text is embedded.
	// Meilisearch's default of 400 bytes would only embed its opening.
	embedTemplateMaxBytes = 8000
)

// settings returns e as a Meilisearch REST embedder. The REST source, rather
// than the openAi one, accepts any model name, which Ollama and Foundry
// deployments need.
func (e Embedder) settings() meilisearch.Embedder {
	return meilisearch.Embedder{
		Source:     meilisearch.RestEmbedderSource,
		URL:        e.URL,
		APIKey:     e.APIKey,
		Dimensions: e.Dimensions,
		// A record whose text is all in its blobs has an empty record
		// chunk, which would otherwise render an empty input, and embeddings
		// APIs such as Azure OpenAI reject that.
		DocumentTemplate: `{% if doc.title != "" %}{{doc.title}}\n{% endif %}` +
			`{% if doc.body != "" %}{{doc.body}}{% else %}{{doc.uri}}{% endif %}`,
		DocumentTemplateMaxBytes: embedTemplateMaxBytes,
		Request: map[string]any{
			"model": e.Model,
			"input": []string{"{{text}}", "{{..}}"},
		},
		Response: map[string]any{
			"data": []any{
				map[string]any{"embedding": "{{embedding}}"},
				"{{..}}",
			},
		},
	}
}

// MeilisearchOption configures a [Meilisearch].
type MeilisearchOption func(*Meilisearch)

// WithEmbedder makes searches hybrid, combining keyword matches with
// semantic matches from embeddings e computes. Without it, any embedder
// configured on the index earlier is removed and searches are keyword only.
func WithEmbedder(e Embedder) MeilisearchOption {
	return func(m *Meilisearch) { m.embedder = &e }
}

// NewMeilisearch returns an Index stored in the Meilisearch indexes uid and
// uid_cursors on the server at host, creating and configuring them if
// needed.
func NewMeilisearch(
	ctx context.Context,
	host, apiKey, uid string,
	opts ...MeilisearchOption,
) (*Meilisearch, error) {
	client := meilisearch.New(host, meilisearch.WithAPIKey(apiKey))
	return createMeilisearch(ctx, client, uid, opts...)
}

// createMeilisearch returns the Index stored in uid and uid_cursors, creating
// them if needed and applying the settings opts describe.
func createMeilisearch(
	ctx context.Context,
	client meilisearch.ServiceManager,
	uid string,
	opts ...MeilisearchOption,
) (*Meilisearch, error) {
	m := newMeilisearchOn(client, uid, opts...)
	settings := indexSettings(m.embedder)
	if err := m.setup(ctx, uid, settings); err != nil {
		return nil, err
	}
	if m.embedder == nil {
		// Embedders are omitted from settings when empty, so drop one left
		// from an earlier configuration explicitly.
		info, err := m.index.ResetEmbeddersWithContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("reset meilisearch embedders: %w", err)
		}
		if err := m.wait(ctx, info); err != nil {
			return nil, fmt.Errorf("reset meilisearch embedders: %w", err)
		}
	}
	if err := m.setup(ctx, uid+"_cursors", &meilisearch.Settings{
		FilterableAttributes: []string{"space"},
	}); err != nil {
		return nil, err
	}
	return m, nil
}

// OpenMeilisearch returns an Index stored in the Meilisearch indexes uid
// and uid_cursors on the server at host. It creates and configures them only
// if they don't exist, and otherwise leaves their settings as they are, so
// opening an index never makes Meilisearch reindex or re-embed it; a
// [Reindexer] applies new settings. Searches are hybrid only while opts has
// an embedder and the index was built with one.
func OpenMeilisearch(
	ctx context.Context,
	host, apiKey, uid string,
	opts ...MeilisearchOption,
) (*Meilisearch, error) {
	client := meilisearch.New(host, meilisearch.WithAPIKey(apiKey))
	if _, err := client.GetIndexWithContext(ctx, uid); isNotFound(err) {
		return createMeilisearch(ctx, client, uid, opts...)
	} else if err != nil {
		return nil, fmt.Errorf("get meilisearch index %s: %w", uid, err)
	}
	m := newMeilisearchOn(client, uid, opts...)
	state, err := m.readState(ctx)
	if err != nil {
		return nil, err
	}
	if state.legacy {
		slog.WarnContext(ctx, "search: the index predates the current projection, so writes "+
			"fail until `pear run reindex` rebuilds it", "index", uid)
	} else if m.embedder != nil && !state.hasEmbedder {
		slog.WarnContext(ctx, "search: the index has no embedder, so searches are keyword only "+
			"until `pear run reindex` rebuilds it", "index", uid)
	}
	m.recheck = &stateCheck{state: state, at: time.Now()}
	return m, nil
}

// readState reads the index's state from its settings.
func (m *Meilisearch) readState(ctx context.Context) (indexState, error) {
	settings, err := m.index.GetSettingsWithContext(ctx)
	if err != nil {
		return indexState{}, fmt.Errorf("get meilisearch settings: %w", err)
	}
	_, has := settings.Embedders[embedderName]
	return indexState{
		hasEmbedder: has,
		legacy:      !slices.Contains(settings.SearchableAttributes, "body"),
	}, nil
}

// state returns the index's state, rereading it at most every
// [stateRecheckInterval].
func (m *Meilisearch) state(ctx context.Context) indexState {
	if m.recheck == nil {
		return indexState{hasEmbedder: m.embedder != nil}
	}
	c := m.recheck
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.at) < stateRecheckInterval {
		return c.state
	}
	state, err := m.readState(ctx)
	if err != nil {
		// Keep what was known; the next call after the interval retries.
		slog.WarnContext(ctx, "search: check index settings failed", "err", err)
	} else {
		c.state = state
	}
	c.at = time.Now()
	return c.state
}

func newMeilisearchOn(
	client meilisearch.ServiceManager,
	uid string,
	opts ...MeilisearchOption,
) *Meilisearch {
	m := &Meilisearch{
		index:   client.Index(uid),
		cursors: client.Index(uid + "_cursors"),
		client:  client,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// isNotFound reports whether err is Meilisearch saying what was asked for
// doesn't exist.
func isNotFound(err error) bool {
	var apiErr *meilisearch.Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// indexSettings returns the settings of the document index, searching with
// embedder when it is set.
func indexSettings(embedder *Embedder) *meilisearch.Settings {
	settings := &meilisearch.Settings{
		SearchableAttributes: searchableAttributes,
		FilterableAttributes: filterableAttributes,
		SortableAttributes:   sortableAttributes,
	}
	if embedder != nil {
		// Meilisearch re-embeds every document when the embedder changes.
		settings.Embedders = map[string]meilisearch.Embedder{
			embedderName: embedder.settings(),
		}
	}
	return settings
}

// setup creates the index uid if it doesn't exist and applies settings.
func (m *Meilisearch) setup(ctx context.Context, uid string, settings *meilisearch.Settings) error {
	info, err := m.client.CreateIndexWithContext(ctx, &meilisearch.IndexConfig{
		Uid:        uid,
		PrimaryKey: "id",
	})
	if err != nil {
		return fmt.Errorf("create meilisearch index %s: %w", uid, err)
	}
	err = m.wait(ctx, info)
	var taskErr *taskError
	if err != nil && (!errors.As(err, &taskErr) || taskErr.code != "index_already_exists") {
		return fmt.Errorf("create meilisearch index %s: %w", uid, err)
	}
	info, err = m.client.Index(uid).UpdateSettingsWithContext(ctx, settings)
	if err != nil {
		return fmt.Errorf("configure meilisearch index %s: %w", uid, err)
	}
	if err := m.wait(ctx, info); err != nil {
		return fmt.Errorf("configure meilisearch index %s: %w", uid, err)
	}
	return nil
}

// Put implements [Index]. Each document is stored as its chunks, written
// before the chunks of its older revision are deleted, so a search never
// misses a record that is being rewritten.
func (m *Meilisearch) Put(ctx context.Context, docs ...Document) error {
	if m.state(ctx).legacy {
		return ErrLegacyIndex
	}
	// Keep the newest revision of each URI in docs.
	newest := make(map[string]Document, len(docs))
	for _, d := range docs {
		id := docID(d.URI)
		if old, ok := newest[id]; !ok || old.Rev < d.Rev {
			newest[id] = d
		}
	}
	if len(newest) == 0 {
		return nil
	}
	// Then drop those older than what's indexed. Every document has a first
	// record chunk, so its revision is read there.
	ids := make([]string, 0, len(newest))
	for parent := range newest {
		ids = append(ids, chunkID(parent, recordSource, 0))
	}
	var existing meilisearch.DocumentsResult
	if err := m.index.GetDocumentsWithContext(ctx, &meilisearch.DocumentsQuery{
		Ids:    ids,
		Fields: []string{"parent_id", "rev"},
		Limit:  int64(len(ids)),
	}, &existing); err != nil {
		return fmt.Errorf("get indexed revisions: %w", err)
	}
	var revs []meiliDoc
	if err := existing.Results.DecodeInto(&revs); err != nil {
		return fmt.Errorf("decode indexed revisions: %w", err)
	}
	// rewritten are the documents indexed at the same revision, such as when
	// a config change reprojects them.
	var rewritten []string
	for _, r := range revs {
		d, ok := newest[r.ParentID]
		switch {
		case !ok:
		case string(d.Rev) < r.Rev:
			delete(newest, r.ParentID)
		case string(d.Rev) == r.Rev:
			rewritten = append(rewritten, r.ParentID)
		}
	}
	if len(newest) == 0 {
		return nil
	}
	var rows []meiliDoc
	stale := make([]string, 0, len(newest))
	for parent, d := range newest {
		rows = append(rows, toMeiliDocs(parent, d)...)
		stale = append(stale, "("+eq("parent_id", parent)+" AND rev != "+quote(d.Rev.String())+")")
	}
	// Read before the new chunks land, so that what isn't rewritten is what
	// they no longer have.
	leftover, err := m.chunkIDs(ctx, rewritten)
	if err != nil {
		return err
	}
	for _, row := range rows {
		delete(leftover, row.ID)
	}
	info, err := m.index.AddDocumentsWithContext(ctx, rows, nil)
	if err != nil {
		return fmt.Errorf("put documents: %w", err)
	}
	if err := m.wait(ctx, info); err != nil {
		return err
	}
	// Chunks of an older revision: ones a shorter text no longer has, or a
	// blob the record dropped.
	info, err = m.index.DeleteDocumentsByFilterWithContext(ctx, strings.Join(stale, " OR "), nil)
	if err != nil {
		return fmt.Errorf("delete stale chunks: %w", err)
	}
	if err := m.wait(ctx, info); err != nil {
		return err
	}
	// Chunks a same-revision rewrite no longer has, which share its rev.
	if len(leftover) == 0 {
		return nil
	}
	info, err = m.index.DeleteDocumentsWithContext(ctx, slices.Collect(maps.Keys(leftover)), nil)
	if err != nil {
		return fmt.Errorf("delete stale chunks: %w", err)
	}
	return m.wait(ctx, info)
}

// chunkIDs returns the ids of every chunk of the documents parents.
func (m *Meilisearch) chunkIDs(ctx context.Context, parents []string) (map[string]bool, error) {
	ids := map[string]bool{}
	if len(parents) == 0 {
		return ids, nil
	}
	const page = 1000
	for offset := int64(0); ; offset += page {
		var res meilisearch.DocumentsResult
		if err := m.index.GetDocumentsWithContext(ctx, &meilisearch.DocumentsQuery{
			Filter: in("parent_id", parents),
			Fields: []string{"id"},
			Offset: offset,
			Limit:  page,
		}, &res); err != nil {
			return nil, fmt.Errorf("get indexed chunks: %w", err)
		}
		var chunks []meiliDoc
		if err := res.Results.DecodeInto(&chunks); err != nil {
			return nil, fmt.Errorf("decode indexed chunks: %w", err)
		}
		for _, c := range chunks {
			ids[c.ID] = true
		}
		if len(chunks) < page {
			return ids, nil
		}
	}
}

// toMeiliDocs returns the chunks of d, whose parent id is parent. The record
// text always has a first chunk, even when empty, so its revision can be
// read.
func toMeiliDocs(parent string, d Document) []meiliDoc {
	var date *int64
	if !d.Date.IsZero() {
		unix := d.Date.Unix()
		date = &unix
	}
	users := stringsOf(d.Users)
	slots := map[Slot]any{}
	for slot, n := range d.Numbers {
		slots[slot] = n
	}
	for slot, t := range d.Datetimes {
		slots[slot] = t.Unix()
	}
	for slot, str := range d.Strings {
		slots[slot] = str
	}
	facets := d.Facets
	if facets == nil {
		facets = []string{}
	}
	base := meiliDoc{
		ParentID:    parent,
		URI:         d.URI.String(),
		SpaceOwner:  d.Space.SpaceOwner().String(),
		SpaceType:   d.Space.SpaceType().String(),
		SpaceKey:    d.Space.Skey().String(),
		Repo:        d.Repo.String(),
		Collection:  d.Collection.String(),
		Rev:         d.Rev.String(),
		Projection:  projectionVersion,
		ConfigRev:   d.ConfigRev,
		Facets:      facets,
		Users:       users,
		Date:        date,
		Slots:       slots,
		meiliAccess: toMeiliAccess(d.Access),
	}
	add := func(rows []meiliDoc, source string, chunks []string) []meiliDoc {
		for n, body := range chunks {
			row := base
			row.ID = chunkID(parent, source, n)
			row.Source = source
			row.Chunk = n
			row.Body = body
			rows = append(rows, row)
		}
		return rows
	}
	record := chunkText(d.Text)
	if len(record) == 0 {
		record = []string{""}
	}
	rows := add(nil, recordSource, record)
	for i := range rows {
		rows[i].Title = d.Title
	}
	rows[0].Extra = d.Extra
	for _, b := range d.Blobs {
		rows = add(rows, blobSource(b.CID), chunkText(b.Text))
	}
	return rows
}

// Delete implements [Index].
func (m *Meilisearch) Delete(ctx context.Context, uris ...habitat_syntax.SpaceRecordURI) error {
	if len(uris) == 0 {
		return nil
	}
	if m.state(ctx).legacy {
		return ErrLegacyIndex
	}
	parents := make([]string, len(uris))
	for i, uri := range uris {
		parents[i] = docID(uri)
	}
	info, err := m.index.DeleteDocumentsByFilterWithContext(ctx, in("parent_id", parents), nil)
	if err != nil {
		return fmt.Errorf("delete documents: %w", err)
	}
	return m.wait(ctx, info)
}

// DeleteSpace implements [Index].
func (m *Meilisearch) DeleteSpace(ctx context.Context, space habitat_syntax.SpaceURI) error {
	// Documents store the parts of their space URI; cursors store it whole.
	for idx, filter := range map[meilisearch.IndexManager]string{
		m.index:   spaceFilter(space),
		m.cursors: eq("space", space.String()),
	} {
		info, err := idx.DeleteDocumentsByFilterWithContext(ctx, filter, nil)
		if err != nil {
			return fmt.Errorf("delete space documents: %w", err)
		}
		if err := m.wait(ctx, info); err != nil {
			return err
		}
	}
	return nil
}

// Cursor implements [Index].
func (m *Meilisearch) Cursor(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) (string, error) {
	var c meiliCursor
	err := m.cursors.GetDocumentWithContext(ctx, cursorID(space, repo), nil, &c)
	var apiErr *meilisearch.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get cursor: %w", err)
	}
	return c.Rev, nil
}

// SetCursor implements [Index].
func (m *Meilisearch) SetCursor(ctx context.Context, cursor RepoCursor) error {
	info, err := m.cursors.AddDocumentsWithContext(ctx, []meiliCursor{{
		ID:    cursorID(cursor.Space, cursor.Repo),
		Space: cursor.Space.String(),
		Repo:  cursor.Repo.String(),
		Rev:   cursor.Rev,
	}}, nil)
	if err != nil {
		return fmt.Errorf("set cursor: %w", err)
	}
	return m.wait(ctx, info)
}

// Cursors implements [Index].
func (m *Meilisearch) Cursors(ctx context.Context) ([]RepoCursor, error) {
	var out []RepoCursor
	for offset := int64(0); ; offset += accessPageSize {
		var page meilisearch.DocumentsResult
		if err := m.cursors.GetDocumentsWithContext(ctx, &meilisearch.DocumentsQuery{
			Limit:  accessPageSize,
			Offset: offset,
		}, &page); err != nil {
			return nil, fmt.Errorf("list cursors: %w", err)
		}
		var rows []meiliCursor
		if err := page.Results.DecodeInto(&rows); err != nil {
			return nil, fmt.Errorf("decode cursors: %w", err)
		}
		for _, r := range rows {
			out = append(out, RepoCursor{
				Space: habitat_syntax.SpaceURI(r.Space),
				Repo:  syntax.DID(r.Repo),
				Rev:   r.Rev,
			})
		}
		if len(rows) < accessPageSize {
			return out, nil
		}
	}
}

// SetSpaceAccess implements [Index].
func (m *Meilisearch) SetSpaceAccess(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	access Access,
) error {
	fields := toMeiliAccess(access)
	// Rewriting the access fields doesn't change which documents match the
	// filter, so paging by offset sees each document once.
	for offset := int64(0); ; offset += accessPageSize {
		var page meilisearch.DocumentsResult
		if err := m.index.GetDocumentsWithContext(ctx, &meilisearch.DocumentsQuery{
			Filter: spaceFilter(space),
			Fields: []string{"id"},
			Limit:  accessPageSize,
			Offset: offset,
		}, &page); err != nil {
			return fmt.Errorf("list space documents: %w", err)
		}
		var ids []meiliDoc
		if err := page.Results.DecodeInto(&ids); err != nil {
			return fmt.Errorf("decode space documents: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		updates := make([]meiliAccessUpdate, len(ids))
		for i, d := range ids {
			updates[i] = meiliAccessUpdate{ID: d.ID, meiliAccess: fields}
		}
		info, err := m.index.UpdateDocumentsWithContext(ctx, updates, nil)
		if err != nil {
			return fmt.Errorf("update space access: %w", err)
		}
		if err := m.wait(ctx, info); err != nil {
			return err
		}
		if len(ids) < accessPageSize {
			return nil
		}
	}
}

// Search implements [Index].
func (m *Meilisearch) Search(ctx context.Context, q Query) (Result, error) {
	offset := 0
	if q.Cursor != "" {
		var err error
		offset, err = strconv.Atoi(q.Cursor)
		if err != nil || offset < 0 {
			return Result{}, ErrInvalidCursor
		}
	}
	if strings.TrimSpace(q.Text) == "" || (q.Reader == nil && len(q.Spaces) == 0) {
		return Result{}, nil
	}
	state := m.state(ctx)
	// An index from before the current projection searches its one text
	// field, and holds one document per record.
	snippetField, distinct := "body", "parent_id"
	if state.legacy {
		snippetField, distinct = "text", ""
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	snippetLength := q.SnippetLength
	if snippetLength <= 0 {
		snippetLength = DefaultSnippetLength
	}
	snippetLength = min(snippetLength, MaxSnippetLength)
	req := &meilisearch.SearchRequest{
		Offset: int64(offset),
		// One extra hit tells whether there is another page.
		Limit:            int64(limit + 1),
		Filter:           filter(q),
		MatchingStrategy: meilisearch.All,
		AttributesToRetrieve: []string{
			"uri", "space_owner", "space_type", "space_key", "repo", "collection",
		},
		AttributesToCrop:      []string{snippetField},
		AttributesToHighlight: []string{snippetField},
		// A record matches once, as its best matching chunk, which gives the
		// snippet.
		Distinct:         distinct,
		CropLength:       int64(snippetLength),
		HighlightPreTag:  "<mark>",
		HighlightPostTag: "</mark>",
	}
	if m.embedder != nil && state.hasEmbedder {
		req.Hybrid = &meilisearch.SearchRequestHybrid{
			Embedder:      embedderName,
			SemanticRatio: m.embedder.SemanticRatio,
		}
		req.RankingScoreThreshold = m.embedder.MinScore
	}
	resp, err := m.index.SearchWithContext(ctx, q.Text, req)
	if err != nil {
		return Result{}, fmt.Errorf("search: %w", err)
	}
	var hits []meiliHit
	if err := resp.Hits.DecodeInto(&hits); err != nil {
		return Result{}, fmt.Errorf("decode hits: %w", err)
	}
	var res Result
	if len(hits) > limit {
		hits = hits[:limit]
		res.Cursor = strconv.Itoa(offset + limit)
	}
	res.Hits = make([]Hit, len(hits))
	for i, h := range hits {
		res.Hits[i] = Hit{
			URI: habitat_syntax.SpaceRecordURI(h.URI),
			Space: habitat_syntax.ConstructSpaceURI(
				syntax.DID(h.SpaceOwner),
				syntax.NSID(h.SpaceType),
				habitat_syntax.SpaceKey(h.SpaceKey),
			),
			Repo:       syntax.DID(h.Repo),
			Collection: syntax.NSID(h.Collection),
			Snippet:    snippet(h, state),
		}
	}
	return res, nil
}

// snippet returns the snippet of hit h from an index in state.
func snippet(h meiliHit, state indexState) string {
	if state.legacy {
		return h.Formatted.Text
	}
	return h.Formatted.Body
}

// filter builds q's Meilisearch filter: an AND of its elements, each
// element an OR of its strings.
func filter(q Query) [][]string {
	var and [][]string
	if q.Reader != nil {
		reader := toMeiliAccess(Access{Principals: *q.Reader})
		or := []string{"public = true"}
		if len(reader.UserReaders) > 0 {
			or = append(or, in("user_readers", reader.UserReaders))
		}
		if len(reader.CommunityRoleReaders) > 0 {
			or = append(or, in("community_role_readers", reader.CommunityRoleReaders))
		}
		and = append(and, or)
	}
	if len(q.Spaces) > 0 {
		or := make([]string, len(q.Spaces))
		for i, space := range q.Spaces {
			or[i] = spaceFilter(space)
		}
		and = append(and, or)
	}
	if len(q.Also) > 0 {
		// Each org matches within its own collections, so the union is an OR of
		// per-org clauses.
		or := []string{scopeFilter(Scope{Org: q.Org, Collections: q.Collections})}
		for _, scope := range q.Also {
			or = append(or, scopeFilter(scope))
		}
		and = append(and, or)
	} else {
		if len(q.Collections) > 0 {
			and = append(and, []string{in("collection", stringsOf(q.Collections))})
		}
		if q.Org != "" {
			and = append(and, []string{eq("space_owner", q.Org.String())})
		}
	}
	if len(q.Repos) > 0 {
		and = append(and, []string{in("repo", stringsOf(q.Repos))})
	}
	return and
}

// scopeFilter matches the documents of scope's org, in its collections.
func scopeFilter(scope Scope) string {
	f := eq("space_owner", scope.Org.String())
	if len(scope.Collections) > 0 {
		f += " AND " + in("collection", stringsOf(scope.Collections))
	}
	return "(" + f + ")"
}

// spaceFilter matches the documents in space, by the parts of its URI.
func spaceFilter(space habitat_syntax.SpaceURI) string {
	return "(" + eq("space_owner", space.SpaceOwner().String()) +
		" AND " + eq("space_type", space.SpaceType().String()) +
		" AND " + eq("space_key", space.Skey().String()) + ")"
}

func toMeiliAccess(a Access) meiliAccess {
	out := meiliAccess{
		UserReaders:          make([]string, len(a.Users)),
		CommunityRoleReaders: make([]string, len(a.CommunityRoles)),
		Public:               a.Public,
	}
	for i, u := range a.Users {
		out.UserReaders[i] = u.String()
	}
	for i, r := range a.CommunityRoles {
		out.CommunityRoleReaders[i] = r.Community.String() + "#" + r.Role
	}
	return out
}

// cursorID is the Meilisearch document id of a repo's cursor.
func cursorID(space habitat_syntax.SpaceURI, repo syntax.DID) string {
	sum := sha256.Sum256([]byte(space.String() + " " + repo.String()))
	return hex.EncodeToString(sum[:])
}

// docID is the Meilisearch document id of uri.
func docID(uri habitat_syntax.SpaceRecordURI) string {
	sum := sha256.Sum256([]byte(uri))
	return hex.EncodeToString(sum[:])
}

// chunkID is the id of chunk n of source in the document parent.
func chunkID(parent, source string, n int) string {
	return parent + "_" + source + "_" + strconv.Itoa(n)
}

// blobSource is the chunk source of the blob c, short enough for an id yet
// distinct among one record's blobs.
func blobSource(c string) string {
	sum := sha256.Sum256([]byte(c))
	return hex.EncodeToString(sum[:6])
}

func stringsOf[T ~string](vs []T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}

// quote quotes v as a Meilisearch filter value.
func quote(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

func eq(field, v string) string {
	return field + " = " + quote(v)
}

func in(field string, vs []string) string {
	quoted := make([]string, len(vs))
	for i, v := range vs {
		quoted[i] = quote(v)
	}
	return field + " IN [" + strings.Join(quoted, ", ") + "]"
}

// taskError is a Meilisearch task that failed.
type taskError struct {
	code    string
	message string
}

func (e *taskError) Error() string {
	return fmt.Sprintf("meilisearch task failed: %s: %s", e.code, e.message)
}

// wait blocks until the task behind info has been applied, so the index
// reads its own writes.
func (m *Meilisearch) wait(ctx context.Context, info *meilisearch.TaskInfo) error {
	return waitTask(ctx, m.client, info)
}

// waitTask blocks until the task behind info has been applied.
func waitTask(
	ctx context.Context,
	client meilisearch.ServiceManager,
	info *meilisearch.TaskInfo,
) error {
	task, err := client.WaitForTaskWithContext(ctx, info.TaskUID, taskPollInterval)
	if err != nil {
		return fmt.Errorf("wait for meilisearch task: %w", err)
	}
	if task.Status != meilisearch.TaskStatusSucceeded {
		return &taskError{code: task.Error.Code, message: task.Error.Message}
	}
	return nil
}
