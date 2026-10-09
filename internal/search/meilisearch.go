package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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
	// snippetWords is about how many words of context a hit's snippet keeps.
	snippetWords = 30
)

// meiliDoc is a [Document] as stored in Meilisearch.
type meiliDoc struct {
	// ID is derived from the URI, which has characters Meilisearch doesn't
	// allow in a document id.
	ID  string `json:"id"`
	URI string `json:"uri"`
	// SpaceOwner, SpaceType and SpaceKey are the parts of the document's
	// space URI, which is not stored whole. Collection scopes are keyed by
	// SpaceOwner.
	SpaceOwner string `json:"space_owner"`
	SpaceType  string `json:"space_type"`
	SpaceKey   string `json:"space_key"`
	Repo       string `json:"repo"`
	Collection string `json:"collection"`
	Rev        string `json:"rev"`
	Text       string `json:"text"`
	meiliAccess
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
}

var _ Index = (*Meilisearch)(nil)

// NewMeilisearch returns an Index stored in the Meilisearch indexes uid and
// uid_cursors on the server at host, creating and configuring them if
// needed.
func NewMeilisearch(ctx context.Context, host, apiKey, uid string) (*Meilisearch, error) {
	client := meilisearch.New(host, meilisearch.WithAPIKey(apiKey))
	m := &Meilisearch{
		index:   client.Index(uid),
		cursors: client.Index(uid + "_cursors"),
		client:  client,
	}
	if err := m.setup(ctx, uid, &meilisearch.Settings{
		SearchableAttributes: []string{"text"},
		FilterableAttributes: []string{
			"space_owner", "space_type", "space_key", "repo", "collection",
			"user_readers", "community_role_readers", "public",
		},
	}); err != nil {
		return nil, err
	}
	if err := m.setup(ctx, uid+"_cursors", &meilisearch.Settings{
		FilterableAttributes: []string{"space"},
	}); err != nil {
		return nil, err
	}
	return m, nil
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

// Put implements [Index].
func (m *Meilisearch) Put(ctx context.Context, docs ...Document) error {
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
	// Then drop those older than what's indexed.
	ids := make([]string, 0, len(newest))
	for id := range newest {
		ids = append(ids, id)
	}
	var existing meilisearch.DocumentsResult
	if err := m.index.GetDocumentsWithContext(ctx, &meilisearch.DocumentsQuery{
		Ids:    ids,
		Fields: []string{"id", "rev"},
		Limit:  int64(len(ids)),
	}, &existing); err != nil {
		return fmt.Errorf("get indexed revisions: %w", err)
	}
	var revs []meiliDoc
	if err := existing.Results.DecodeInto(&revs); err != nil {
		return fmt.Errorf("decode indexed revisions: %w", err)
	}
	for _, r := range revs {
		if d, ok := newest[r.ID]; ok && string(d.Rev) < r.Rev {
			delete(newest, r.ID)
		}
	}
	if len(newest) == 0 {
		return nil
	}
	rows := make([]meiliDoc, 0, len(newest))
	for id, d := range newest {
		rows = append(rows, meiliDoc{
			ID:          id,
			URI:         d.URI.String(),
			SpaceOwner:  d.Space.SpaceOwner().String(),
			SpaceType:   d.Space.SpaceType().String(),
			SpaceKey:    d.Space.Skey().String(),
			Repo:        d.Repo.String(),
			Collection:  d.Collection.String(),
			Rev:         d.Rev.String(),
			Text:        d.Text,
			meiliAccess: toMeiliAccess(d.Access),
		})
	}
	info, err := m.index.AddDocumentsWithContext(ctx, rows, nil)
	if err != nil {
		return fmt.Errorf("put documents: %w", err)
	}
	return m.wait(ctx, info)
}

// Delete implements [Index].
func (m *Meilisearch) Delete(ctx context.Context, uris ...habitat_syntax.SpaceRecordURI) error {
	if len(uris) == 0 {
		return nil
	}
	ids := make([]string, len(uris))
	for i, uri := range uris {
		ids[i] = docID(uri)
	}
	info, err := m.index.DeleteDocumentsWithContext(ctx, ids, nil)
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
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	resp, err := m.index.SearchWithContext(ctx, q.Text, &meilisearch.SearchRequest{
		Offset: int64(offset),
		// One extra hit tells whether there is another page.
		Limit:            int64(limit + 1),
		Filter:           filter(q),
		MatchingStrategy: meilisearch.All,
		AttributesToRetrieve: []string{
			"uri", "space_owner", "space_type", "space_key", "repo", "collection",
		},
		AttributesToCrop:      []string{"text"},
		AttributesToHighlight: []string{"text"},
		CropLength:            snippetWords,
		HighlightPreTag:       "<mark>",
		HighlightPostTag:      "</mark>",
	})
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
			Snippet:    h.Formatted.Text,
		}
	}
	return res, nil
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
	task, err := m.client.WaitForTaskWithContext(ctx, info.TaskUID, taskPollInterval)
	if err != nil {
		return fmt.Errorf("wait for meilisearch task: %w", err)
	}
	if task.Status != meilisearch.TaskStatusSucceeded {
		return &taskError{code: task.Error.Code, message: task.Error.Message}
	}
	return nil
}
