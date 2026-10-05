// Package search indexes the text of space records for full-text search.
//
// Each indexed document carries its space's [Access]: who its permission
// records let read it. That is the users the perms store resolves as readers,
// flattening network.habitat.relationship.userRelation and spaceRelation
// records, and the community roles in community.opensocial.access. A query runs as a [Reader] and
// only matches documents whose Access includes one of the reader's
// principals, so the index enforces read permission itself. Callers still
// hydrate hits from the spaces store, which is the source of truth for the
// record values.
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
	// Access is who may read the document: its space's Access.
	Access Access
}

// CommunityRole names the members of an opensocial community holding the
// community.opensocial.role with record key Role.
type CommunityRole struct {
	Community syntax.DID
	Role      string
}

// Principals is a set of identities, each matched as a whole: a user or a
// role in a community.
type Principals struct {
	Users          []syntax.DID
	CommunityRoles []CommunityRole
}

// Access is who may read a space's documents.
type Access struct {
	// Principals granted read: the users the perms store resolves as the
	// space's readers, and the community roles in its
	// community.opensocial.access record.
	Principals
	// Public is set when anyone may read, as with an opensocial about space.
	Public bool
}

// Reader is who a query runs as: every principal the caller holds. A
// document matches when its Access is public or shares a principal with the
// reader.
type Reader = Principals

// Query selects documents to return.
type Query struct {
	// Text is free-form user input, not index syntax.
	Text string
	// Reader limits results to documents it may read. A nil Reader skips the
	// permission filter, for callers that authorized Spaces themselves; it
	// then requires Spaces, so an index is never searched unscoped.
	Reader *Reader
	// Spaces, Collections and Repos optionally narrow results further.
	Spaces      []habitat_syntax.SpaceURI
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

// RepoCursor is the last revision of a repo the index holds.
type RepoCursor struct {
	Space habitat_syntax.SpaceURI
	Repo  syntax.DID
	Rev   string
}

// Index stores documents and searches them. It also keeps how far it has read
// each repo, so the cursors are lost exactly when the documents are.
// Implementations must be safe for concurrent use.
type Index interface {
	// Put inserts or replaces docs.
	Put(ctx context.Context, docs ...Document) error
	// Delete removes the documents with the given URIs; missing ones are
	// ignored.
	Delete(ctx context.Context, uris ...habitat_syntax.SpaceRecordURI) error
	// DeleteSpace removes every document and cursor in a space.
	DeleteSpace(ctx context.Context, space habitat_syntax.SpaceURI) error
	// SetSpaceAccess replaces the Access of every document in a space, after
	// its permission records change.
	SetSpaceAccess(ctx context.Context, space habitat_syntax.SpaceURI, access Access) error
	// Search returns the documents matching q, most relevant first.
	Search(ctx context.Context, q Query) (Result, error)

	// Cursor returns how far the index has read repo, or "" if not at all.
	Cursor(ctx context.Context, space habitat_syntax.SpaceURI, repo syntax.DID) (string, error)
	// SetCursor records how far the index has read a repo.
	SetCursor(ctx context.Context, cursor RepoCursor) error
	// Cursors lists the cursor of every repo the index has read.
	Cursors(ctx context.Context) ([]RepoCursor, error)
}
