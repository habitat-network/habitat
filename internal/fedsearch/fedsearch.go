// Package fedsearch runs one query across many search backends at once and
// ranks their results together.
//
// A [Backend] is one searchable source bound to a caller, such as Habitat's
// record index or one search tool of a member's connected MCP server. A
// [Resolver] lists the backends of one kind that a caller can search. The
// [Service] starts every backend of a search in the background and hands back
// a [Search], which serves pages as results arrive: a call waits up to a
// timeout and returns what is ready, while slower backends keep running. A
// backend that fails is reported in the page's backend states rather than
// failing the search.
//
// A search has a mixed stream, every backend's results ranked against each
// other, and a stream per backend. Results are scored by a reranker when they
// arrive and merged by score into what the mixed stream hasn't served yet, so
// pages already served never change and a late, relevant result still lands
// at the top of the next page. Cursors ([Cursor]) name a stream and an offset
// into it.
//
// Permissions are each backend's job: a backend returns only what its caller
// may read, and the service only merges. A search is bound to its caller, and
// its state is kept in an encrypted keyval store, so a search outlives a
// restart; a backend still running at a restart is reported as interrupted.
package fedsearch

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// BackendID identifies a backend within a search, stably across searches,
// such as "records" or "mcp:slack/slack-messages".
type BackendID string

// Kind is a type of backend, such as "records" or "mcp".
type Kind string

// ResultKind says what a result is, so a client knows how to read its Data.
type ResultKind string

// Backend is one searchable source, already bound to a caller and an org.
// Implementations must be safe for concurrent use.
type Backend interface {
	// ID identifies the backend; see BackendID.
	ID() BackendID
	// Kind is the backend's type.
	Kind() Kind
	// Name is a display name, such as "Slack messages".
	Name() string
	// Search returns the backend's results for q, most relevant first by the
	// backend's own ranking, and at most q.Limit of them. It blocks until the
	// backend answers or ctx is done, runs as the caller, and returns only
	// what the caller may read. Return an error wrapping [ErrNotConnected]
	// when the caller has no usable connection to the backend.
	Search(ctx context.Context, q Query) ([]Result, error)
}

// Resolver lists the backends of one kind that a caller can search in an org.
type Resolver interface {
	// Kind is the kind of the backends it lists.
	Kind() Kind
	// Backends lists caller's backends in org. It may return none.
	Backends(ctx context.Context, caller, org syntax.DID) ([]Backend, error)
}

// Query is what to search every backend for.
type Query struct {
	// Text is free-form user input.
	Text string
	// Limit is how many results to ask of each backend; zero means
	// [DefaultBackendLimit].
	Limit int
}

// Result is one search result, in the shape every backend returns.
type Result struct {
	// Backend is the backend the result came from. The service sets it.
	Backend BackendID  `json:"backend"`
	Kind    ResultKind `json:"kind"`
	// ID identifies the result within its backend, such as a record URI or
	// an item's URL.
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	URL     string `json:"url,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	// Data is the backend's own payload for the result, passed through as is
	// (for a record, its space and value).
	Data json.RawMessage `json:"data,omitempty"`
	// Score is the result's relevance to the query, set by the service when
	// it ranks the result. Scores are comparable across the backends of one
	// search.
	Score float64 `json:"score"`
}

// Status is how a backend's search stands.
type Status string

const (
	// StatusPending means the backend hasn't answered yet.
	StatusPending Status = "pending"
	// StatusOK means the backend answered; it may still have found nothing.
	StatusOK Status = "ok"
	// StatusError means the backend failed.
	StatusError Status = "error"
	// StatusTimeout means the backend didn't answer within the service's
	// backend timeout.
	StatusTimeout Status = "timeout"
	// StatusNotConnected means the caller has no usable connection to the
	// backend, for example because their sign-in expired.
	StatusNotConnected Status = "not_connected"
)

// ErrorCode classifies a backend's failure.
type ErrorCode string

const (
	// CodeUpstreamError means the backend itself failed.
	CodeUpstreamError ErrorCode = "upstream_error"
	// CodeTimeout means the backend didn't answer in time.
	CodeTimeout ErrorCode = "timeout"
	// CodeNotConnected means the caller isn't connected to the backend.
	CodeNotConnected ErrorCode = "not_connected"
	// CodeInterrupted means pear restarted while the backend was running.
	CodeInterrupted ErrorCode = "interrupted"
)

// BackendError describes why a backend has no results.
type BackendError struct {
	Code ErrorCode `json:"code"`
	// Message is short and safe to show the caller.
	Message string `json:"message"`
}

// BackendState is how one backend of a search stands.
type BackendState struct {
	ID     BackendID `json:"id"`
	Kind   Kind      `json:"kind"`
	Name   string    `json:"name"`
	Status Status    `json:"status"`
	// Error is set for every status but pending and ok.
	Error *BackendError `json:"error,omitempty"`
	// Count is how many results the backend returned, once ok.
	Count int `json:"count"`
	// Elapsed is how long the backend took, once it is no longer pending.
	Elapsed time.Duration `json:"elapsed"`
}

// Ranking says how a search's results were scored.
type Ranking string

const (
	// RankingReranker means the reranker scored every result.
	RankingReranker Ranking = "reranker"
	// RankingFallback means there is no reranker, or it failed for some
	// results, which were scored by their rank within their backend instead.
	// Backends then interleave in their own orders.
	RankingFallback Ranking = "fallback"
)

const (
	// DefaultBackendLimit is how many results each backend is asked for when
	// the query gives no limit.
	DefaultBackendLimit = 10
	// MaxBackendLimit is the most results a query can ask of each backend.
	MaxBackendLimit = 50
	// DefaultBackendTimeout bounds each backend's search, which runs in the
	// background past any one call's timeout.
	DefaultBackendTimeout = 30 * time.Second
	// DefaultTTL is how long a search is kept after its last use.
	DefaultTTL = 10 * time.Minute
)

var (
	// ErrNotConnected is wrapped by a backend's error when the caller has no
	// usable connection to it.
	ErrNotConnected = errors.New("not connected")
	// ErrEmptyQuery is returned when searching for nothing.
	ErrEmptyQuery = errors.New("query is required")
	// ErrInvalidLimit is returned for a backend limit outside 0 to
	// MaxBackendLimit.
	ErrInvalidLimit = errors.New("limit is out of range")
	// ErrNotFound is returned for a search that is unknown, expired or
	// another caller's.
	ErrNotFound = errors.New("search not found")
	// ErrInvalidCursor is returned for a cursor that can't be decoded, or
	// that points past what its stream has served or at a backend the search
	// doesn't have.
	ErrInvalidCursor = errors.New("invalid cursor")
)
