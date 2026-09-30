package search

import (
	"context"
	"fmt"
	"slices"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// SpaceLister lists the spaces a DID holds a role on. [perms.Store]
// implements it.
type SpaceLister interface {
	ListObjects(
		ctx context.Context,
		did syntax.DID,
		role habitat_syntax.SpaceRole,
		filterType *syntax.NSID,
	) ([]habitat_syntax.SpaceURI, error)
}

// RecordGetter batch-fetches records. [spaces.Store] implements it.
type RecordGetter interface {
	GetRecords(
		ctx context.Context,
		uris []habitat_syntax.SpaceRecordURI,
	) ([]spaces.Record, error)
}

// Match is one search result: the record as the spaces store holds it now,
// with the snippet the index matched.
type Match struct {
	URI     habitat_syntax.SpaceRecordURI
	Record  spaces.Record
	Snippet string
}

// Page is one page of matches.
type Page struct {
	Matches []Match
	// Cursor is passed as [Query.Cursor] to fetch the next page; empty when
	// there are no more matches.
	Cursor string
}

// Searcher answers searches on behalf of a caller. The [Index] holds no
// permissions, so Searcher scopes each query to the spaces the caller can
// read, and returns records from the spaces store rather than the index.
type Searcher struct {
	index   Index
	perms   SpaceLister
	records RecordGetter
}

// NewSearcher returns a Searcher over index, checking reads with perms and
// loading matched records from records.
func NewSearcher(index Index, perms SpaceLister, records RecordGetter) *Searcher {
	return &Searcher{index: index, perms: perms, records: records}
}

// Search runs q over the spaces reader can read: q.Spaces narrowed to those,
// or all of them when q.Spaces is empty. A space reader can't read is left
// out rather than rejected, so it can't be told apart from one with no
// matches.
func (s *Searcher) Search(ctx context.Context, reader syntax.DID, q Query) (Page, error) {
	readable, err := s.perms.ListObjects(ctx, reader, habitat_syntax.SpaceRoleReader, nil)
	if err != nil {
		return Page{}, fmt.Errorf("list readable spaces: %w", err)
	}
	if len(q.Spaces) > 0 {
		readable = slices.DeleteFunc(readable, func(space habitat_syntax.SpaceURI) bool {
			return !slices.Contains(q.Spaces, space)
		})
	}
	q.Spaces = readable
	return s.SearchSpaces(ctx, q)
}

// SearchSpaces runs q over q.Spaces without checking who may read them, for
// callers that have already authorized them, such as with a space
// credential.
func (s *Searcher) SearchSpaces(ctx context.Context, q Query) (Page, error) {
	if len(q.Spaces) == 0 {
		return Page{}, nil
	}
	result, err := s.index.Search(ctx, q)
	if err != nil {
		return Page{}, fmt.Errorf("search index: %w", err)
	}
	uris := make([]habitat_syntax.SpaceRecordURI, len(result.Hits))
	for i, hit := range result.Hits {
		uris[i] = hit.URI
	}
	records, err := s.records.GetRecords(ctx, uris)
	if err != nil {
		return Page{}, fmt.Errorf("get matched records: %w", err)
	}
	byURI := make(map[habitat_syntax.SpaceRecordURI]spaces.Record, len(records))
	for _, rec := range records {
		uri := habitat_syntax.ConstructSpaceRecordURI(
			rec.Space,
			rec.Owner,
			rec.Collection,
			rec.Rkey,
		)
		byURI[uri] = rec
	}
	// The index trails the store, so a hit can name a record deleted since it
	// was indexed. Drop those: a page can come back short, but the cursor
	// still continues past them.
	page := Page{Cursor: result.Cursor}
	for _, hit := range result.Hits {
		rec, ok := byURI[hit.URI]
		if !ok {
			continue
		}
		page.Matches = append(page.Matches, Match{URI: hit.URI, Record: rec, Snippet: hit.Snippet})
	}
	return page, nil
}
