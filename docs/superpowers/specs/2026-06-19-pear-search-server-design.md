# Pear Search Server Design

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a search server, deployed alongside `pear` (same docker-compose, separate process/database), that indexes records across all orgs via `subscribeSpaces`/`listRecords` and exposes a full-text search query API to the frontend and agents, proxied through pear.

**Architecture:** A new `cmd/search` binary with its own Postgres database. It authenticates to pear using an instance-wide M2M OAuth credential (designed separately) to open a long-lived `subscribeSpaces` stream and backfill via `listRecords`, extracting searchable text into a Postgres full-text-search table behind a swappable `search.Index` interface. A new lexicon `network.habitat.search.query` is served by the search server and reached by clients through a new forwarding hop in pear (`/xrpc/network.habitat.search.*` → search server), so the public API surface is unchanged. Query-time permissioning is primitive in v1: the search server resolves the caller's org via pear's existing `network.habitat.org.getMetadata` and filters results to that org.

**Tech Stack:** Go, Postgres (full-text search), AT Protocol lexicons, GORM.

**Out of scope / deferred:**
- Space-level (not just org-level) permission filtering — revisit once `subscribeSpaces` carries permission-change events.
- The M2M auth credential design itself (already settled separately: an instance-wide `client_credentials` OAuth client).

---

## Changes

### 1. `search.Index` interface — swappable backend

**File:** `internal/search/index.go` (new)

```go
package search

import (
    "context"
    "time"
)

type Document struct {
    URI        string // record URI, primary identity of a document
    SpaceURI   string
    OrgDID     string
    RecordType string // NSID
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

// Index is the storage/query backend for indexed records. The Postgres
// full-text implementation is the first backend; a future pgvector+Ollama
// implementation satisfies the same interface so the ingestion pipeline and
// HTTP handler never change.
type Index interface {
    Upsert(ctx context.Context, doc Document) error
    Delete(ctx context.Context, uri string) error
    Query(ctx context.Context, params QueryParams) (QueryResult, error)
}
```

### 2. Postgres FTS implementation

**File:** `internal/search/postgres_fts.go` (new)

GORM model:

```go
type searchDocument struct {
    URI        string `gorm:"primaryKey"`
    SpaceURI   string `gorm:"index"`
    OrgDID     string `gorm:"index"`
    RecordType string
    Content    string
    UpdatedAt  time.Time
    // tsv is a generated column; created via raw SQL migration, not gorm tags
}
```

Migration (raw SQL, run once via `AutoMigrate` + a follow-up `Exec`):

```sql
ALTER TABLE search_documents
  ADD COLUMN IF NOT EXISTS tsv tsvector
  GENERATED ALWAYS AS (to_tsvector('english', content)) STORED;
CREATE INDEX IF NOT EXISTS search_documents_tsv_idx ON search_documents USING GIN (tsv);
```

`postgresFTSIndex` implements `Index`:
- `Upsert` — `ON CONFLICT (uri) DO UPDATE` on all fields except `tsv` (generated).
- `Delete` — delete by `uri`.
- `Query` — 
  ```sql
  SELECT uri, space_uri, record_type,
         ts_rank(tsv, query) AS rank,
         ts_headline('english', content, query) AS snippet
  FROM search_documents, websearch_to_tsquery('english', $queryText) query
  WHERE org_did = $orgDID AND tsv @@ query
  ORDER BY rank DESC
  LIMIT $limit OFFSET $cursorOffset
  ```
  Cursor is a simple offset encoding for v1 (consistent with "primitive for now"); can move to a rank/uri keyset cursor later without changing the `Index` interface.

### 3. Indexing pipeline

**File:** `internal/search/indexer.go` (new)

```go
type Indexer struct {
    index      Index
    pearClient *http.Client // authenticated with the M2M credential
    pearHost   string
}

func (i *Indexer) Run(ctx context.Context) error
```

- Connects to `{pearHost}/xrpc/network.habitat.sync.subscribeSpaces` via SSE (same client shape as `internal/sap/subscriber.go`'s `sse.NewClient` usage), using `Last-Event-ID`/`cursor` to resume from the last persisted sequence number (store the cursor in a small `search_cursor` table — one row).
- On each `space` event: parse the space URI, derive `OrgDID` from the URI's owner segment, fetch the record via `getRecord` if the event doesn't carry full content, extract `Content` (v1: flatten all string-valued fields in the record JSON via a generic walk), and call `index.Upsert`. Deletion events call `index.Delete`.
- On startup with no persisted cursor (first run), backfill: call `listSpaces`/`listRecords` across all orgs to populate the index before switching to the live stream — mirrors how `subscribeSpaces` cursors already support resuming, so backfill is just "start from cursor 0."

### 4. Lexicon — `network.habitat.search.query`

**File:** `lexicons/network/habitat/search/query.json` (new)

```json
{
  "lexicon": 1,
  "id": "network.habitat.search.query",
  "defs": {
    "main": {
      "type": "query",
      "parameters": {
        "type": "params",
        "properties": {
          "q": { "type": "string" },
          "limit": { "type": "integer", "default": 25 },
          "cursor": { "type": "string" }
        },
        "required": ["q"]
      },
      "output": {
        "encoding": "application/json",
        "schema": {
          "type": "object",
          "properties": {
            "results": {
              "type": "array",
              "items": { "type": "ref", "ref": "#resultView" }
            },
            "cursor": { "type": "string" }
          }
        }
      }
    },
    "resultView": {
      "type": "object",
      "properties": {
        "uri": { "type": "string" },
        "spaceUri": { "type": "string" },
        "recordType": { "type": "string" },
        "snippet": { "type": "string" },
        "rank": { "type": "number" }
      }
    }
  }
}
```

Run `moon :generate` to produce Go/TS bindings (`habitat.NetworkHabitatSearchQuery*`), matching the existing pattern for every other endpoint.

### 5. Search server HTTP handler

**File:** `internal/search/server.go` (new)

```go
type Server struct {
    index    Index
    orgStore orgResolver // calls pear's getMetadata, see below
}

func (s *Server) Query(w http.ResponseWriter, r *http.Request) {
    // 1. forward caller's Authorization header to pear's
    //    network.habitat.org.getMetadata to resolve org DID (cached briefly)
    // 2. decode q/limit/cursor params
    // 3. index.Query(ctx, QueryParams{OrgDID: callerOrg, ...})
    // 4. encode response per the lexicon
}
```

`orgResolver` is a small interface (`ResolveCallerOrg(ctx, bearerToken string) (string, error)`) backed by an HTTP call to `{pearHost}/xrpc/network.habitat.org.getMetadata` (no `orgId` param) with the caller's forwarded token, with an in-memory TTL cache (~30–60s) keyed by token to avoid a round trip per query.

### 6. `cmd/search` binary

**File:** `cmd/search/main.go` (new)

Standard layout matching other `cmd/` entrypoints: parse flags/env (Postgres DSN, pear host, M2M client credentials), construct `postgresFTSIndex`, `Indexer`, `search.Server`, start the indexer in a goroutine, serve the HTTP handler.

### 7. Forwarding hop in pear

**File:** `internal/forwarding/search_proxy.go` (new)

Structurally simpler than `serviceProxy`/`pdsForwarding` — no signing, just a reverse proxy for everything under `/xrpc/network.habitat.search.`, forwarding the original `Authorization` header unchanged to the search server's internal address.

**File:** `cmd/pear/main.go`

```go
searchProxy := forwarding.NewSearchProxy(searchServerHost)
mux.PathPrefix("/xrpc/network.habitat.search.").Handler(searchProxy)
```

### 8. Docker-compose wiring

Add a `search` service (the new `cmd/search` binary) and its own Postgres database/service (or separate DB on the existing Postgres instance) to the self-hosting compose file. Configure `SEARCH_HOST` (or similar) on `pear` so the forwarding hop knows where to send requests, and the M2M client credentials identically on both.

---

## File Change Summary

| File | Change |
|---|---|
| `internal/search/index.go` | New — `Index` interface, `Document`/`QueryParams`/`Result` types |
| `internal/search/postgres_fts.go` | New — Postgres full-text `Index` implementation |
| `internal/search/indexer.go` | New — `subscribeSpaces` consumer + backfill, drives `Index.Upsert`/`Delete` |
| `internal/search/server.go` | New — HTTP handler for `network.habitat.search.query`, org-resolving permission filter |
| `lexicons/network/habitat/search/query.json` | New — lexicon definition |
| `cmd/search/main.go` | New — search server entrypoint |
| `internal/forwarding/search_proxy.go` | New — reverse-proxy hop for `/xrpc/network.habitat.search.*` |
| `cmd/pear/main.go` | Wire up `search_proxy` route |
| docker-compose (self-hosting) | New `search` service + database, shared M2M secret env vars |
