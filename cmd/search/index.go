package main

import (
	"context"
	"time"
)

// Document is a single indexed record.
type Document struct {
	URI        string // record URI, primary identity of a document
	SpaceURI   string
	OrgDID     string
	RecordType string // NSID of the record
	Content    string // extracted searchable text
	UpdatedAt  time.Time
}

type QueryParams struct {
	OrgDID    string
	QueryText string
	Limit     int
	Cursor    string
}

type Result struct {
	URI        string
	SpaceURI   string
	RecordType string
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
	Delete(ctx context.Context, uri string) error
	Query(ctx context.Context, params QueryParams) (QueryResult, error)
}
