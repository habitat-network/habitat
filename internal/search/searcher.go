package search

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// CommunitySource lists a user's opensocial community roles.
// [opensocial.Store] implements it.
type CommunitySource interface {
	// ListMemberSpaces returns the members space of every community user
	// belongs to.
	ListMemberSpaces(ctx context.Context, user syntax.DID) ([]habitat_syntax.SpaceURI, error)
	// GetUserRoles returns the role record keys user holds in a community.
	GetUserRoles(ctx context.Context, orgDID syntax.DID, user syntax.DID) ([]string, error)
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

// Searcher answers searches on behalf of a caller: it resolves the caller's
// [Reader], so the index only matches what they may read, and returns
// records from the spaces store rather than the index.
type Searcher struct {
	index       Index
	communities CommunitySource
	records     RecordGetter
}

// NewSearcher returns a Searcher over index, resolving callers' community
// roles with communities and loading matched records from records.
func NewSearcher(index Index, communities CommunitySource, records RecordGetter) *Searcher {
	return &Searcher{index: index, communities: communities, records: records}
}

// Search runs q as caller, over every space they may read, narrowed to
// q.Spaces when set. A space caller can't read finds nothing rather than
// failing, so it can't be told apart from one with no matches.
func (s *Searcher) Search(ctx context.Context, caller syntax.DID, q Query) (Page, error) {
	reader, err := s.reader(ctx, caller)
	if err != nil {
		return Page{}, err
	}
	q.Reader = &reader
	return s.search(ctx, q)
}

// SearchSpaces runs q over q.Spaces without checking who may read them, for
// callers that have already authorized them, such as with a space
// credential.
func (s *Searcher) SearchSpaces(ctx context.Context, q Query) (Page, error) {
	q.Reader = nil
	return s.search(ctx, q)
}

// reader returns caller's principals: themselves, and their roles in each
// community they belong to. Readers granted through spaceRelations are
// already flattened to users in the index.
func (s *Searcher) reader(ctx context.Context, caller syntax.DID) (Reader, error) {
	reader := Reader{Users: []syntax.DID{caller}}
	memberSpaces, err := s.communities.ListMemberSpaces(ctx, caller)
	if err != nil {
		return Reader{}, fmt.Errorf("list communities: %w", err)
	}
	for _, members := range memberSpaces {
		community := members.SpaceOwner()
		roles, err := s.communities.GetUserRoles(ctx, community, caller)
		if err != nil {
			return Reader{}, fmt.Errorf("get community roles: %w", err)
		}
		for _, role := range roles {
			reader.CommunityRoles = append(reader.CommunityRoles,
				CommunityRole{Community: community, Role: role})
		}
	}
	return reader, nil
}

func (s *Searcher) search(ctx context.Context, q Query) (Page, error) {
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
