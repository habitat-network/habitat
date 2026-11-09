package mcpsearch

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/search"
)

// RecordSearcher runs a record search as a caller. See search.Searcher for
// the concrete implementation.
type RecordSearcher interface {
	Search(ctx context.Context, caller syntax.DID, q search.Query) (search.Page, error)
}

// RecordsBackend is the Backend that searches the org's Habitat records, as
// the caller, so it only matches records they may read.
type RecordsBackend struct {
	searcher RecordSearcher
}

var _ Backend = (*RecordsBackend)(nil)

// NewRecordsBackend returns a Backend searching with searcher.
func NewRecordsBackend(searcher RecordSearcher) *RecordsBackend {
	return &RecordsBackend{searcher: searcher}
}

// Search implements Backend.
func (b *RecordsBackend) Search(ctx context.Context, req Request) Outcome {
	q := search.Query{
		Text:          req.Query.Text,
		Org:           req.Org,
		Limit:         req.Query.Limit,
		SnippetLength: req.Query.SnippetLength,
	}
	if req.Query.Collection != "" {
		q.Collections = []syntax.NSID{req.Query.Collection}
	}
	source := Source{Provider: ProviderRecords}
	page, err := b.searcher.Search(ctx, req.Caller, q)
	if err != nil {
		source.Status, source.Error = StatusError, err.Error()
		return Outcome{Sources: []Source{source}}
	}
	itemSource := ItemSource{Provider: ProviderRecords}
	items := make([]Item, len(page.Matches))
	for i, m := range page.Matches {
		items[i] = Item{
			Source:  itemSource,
			URL:     m.URI.String(),
			Snippet: m.Snippet,
			Record: &Record{
				Space: m.Record.Space.String(),
				CID:   m.Record.Cid.String(),
				Value: m.Record.Value,
			},
		}
	}
	source.Status, source.Count = StatusOK, len(items)
	return Outcome{Items: items, Sources: []Source{source}}
}
