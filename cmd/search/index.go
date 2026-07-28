package main

import (
	"context"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Document is a single indexed record.
type Document struct {
	URI        habitat_syntax.SpaceRecordURI // record URI, primary identity of a document
	SpaceURI   habitat_syntax.SpaceURI
	OrgDID     syntax.DID
	Collection syntax.NSID // NSID of the record
	Content    string      // extracted searchable text
	UpdatedAt  time.Time
}

type QueryParams struct {
	OrgDID    syntax.DID
	QueryText string
	Limit     int
	Cursor    string
}

type Result struct {
	URI        habitat_syntax.SpaceRecordURI
	SpaceURI   habitat_syntax.SpaceURI
	Collection syntax.NSID // NSID of the record
	Snippet    string
	Rank       float64
}

type QueryResult struct {
	Results    []Result
	NextCursor string
}

// Index is the storage/query backend for indexed records. postgresFTSIndex
// (full-text search) is the only implementation today; a future
// pgvector+Ollama implementation satisfies the same interface so the
// indexing pipeline and HTTP handler never change.
type Index interface {
	Upsert(ctx context.Context, doc Document) error
	Delete(ctx context.Context, uri habitat_syntax.SpaceRecordURI) error
	Query(ctx context.Context, params QueryParams) (QueryResult, error)
}
