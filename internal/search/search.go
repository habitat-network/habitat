// Package search indexes the text of space records for full-text search.
//
// The index only stores text and enough metadata to filter it (space, repo,
// collection). It knows nothing about permissions: callers must pass the
// spaces the requester may read in [Query.Spaces] and hydrate hits from the
// spaces store, so revoking access takes effect without touching the index.
package search

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Document is the searchable text of one record.
type Document struct {
	URI        habitat_syntax.SpaceRecordURI
	Space      habitat_syntax.SpaceURI
	Repo       syntax.DID
	Collection syntax.NSID
	// Rev is the record's revision. Put ignores a document older than the one
	// already indexed for its URI, so replaying ops out of order is harmless.
	Rev syntax.TID
	// Text is the record's extracted text; see [ExtractText].
	Text string
}

// Query selects documents to return.
type Query struct {
	// Text is free-form user input, not index syntax.
	Text string
	// Spaces restricts results to these spaces. It is required: a query with
	// no spaces returns nothing, so an index can never be searched unscoped.
	Spaces []habitat_syntax.SpaceURI
	// Collections and Repos optionally narrow results further.
	Collections []syntax.NSID
	Repos       []syntax.DID
	// Limit caps the hits returned; zero means [DefaultLimit].
	Limit int
	// Cursor continues a previous result's [Result.Cursor].
	Cursor string
}

// DefaultLimit is the page size used when [Query.Limit] is zero.
const DefaultLimit = 25

// Hit is one matching document.
type Hit struct {
	URI        habitat_syntax.SpaceRecordURI
	Space      habitat_syntax.SpaceURI
	Repo       syntax.DID
	Collection syntax.NSID
	// Score orders hits, higher being more relevant. Scores from different
	// implementations aren't comparable.
	Score float64
	// Snippet is a fragment of the record's text with matches wrapped in
	// <mark></mark>. The rest is unescaped record text, so escape it before
	// rendering as HTML.
	Snippet string
}

// Result is one page of hits.
type Result struct {
	Hits []Hit
	// Cursor is passed as [Query.Cursor] to fetch the next page; empty when
	// there are no more hits.
	Cursor string
}

// Index stores documents and searches them. Implementations must be safe for
// concurrent use.
type Index interface {
	// Put inserts or replaces docs.
	Put(ctx context.Context, docs ...Document) error
	// Delete removes the documents with the given URIs; missing ones are
	// ignored.
	Delete(ctx context.Context, uris ...habitat_syntax.SpaceRecordURI) error
	// DeleteSpace removes every document in a space.
	DeleteSpace(ctx context.Context, space habitat_syntax.SpaceURI) error
	// Search returns the documents matching q, most relevant first.
	Search(ctx context.Context, q Query) (Result, error)
}
