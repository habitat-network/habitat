package pearserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/fedsearch"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/search"
)

// These match the network.habitat.mcp.search lexicon.
const (
	maxMCPSearchQueryLength = 1000
	defaultMCPSearchLimit   = 10
	maxMCPSearchLimit       = 50
	defaultMCPSearchTimeout = 10 * time.Second
	maxMCPSearchTimeout     = 30 * time.Second
	maxMCPSearchBodyBytes   = 64 << 10
)

// McpSearch implements network.habitat.mcp.search: a federated search over
// Habitat records and the caller's connected MCP servers (see
// internal/fedsearch).
func (p *PearServer) McpSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxMCPSearchBodyBytes))
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "reading request body", err)
		return
	}
	var input habitat.NetworkHabitatMcpSearchInput
	// The generated input can't tell an explicit timeoutMs of 0, to return
	// what's ready now, from an omitted one, for the default.
	var timeoutMs struct {
		TimeoutMs *int64 `json:"timeoutMs"`
	}
	if err := json.Unmarshal(body, &input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "reading request body", err)
		return
	}
	_ = json.Unmarshal(body, &timeoutMs) // it parsed above
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	limit, timeout := int(input.Limit), defaultMCPSearchTimeout
	if limit == 0 {
		limit = defaultMCPSearchLimit
	}
	if timeoutMs.TimeoutMs != nil {
		timeout = time.Duration(*timeoutMs.TimeoutMs) * time.Millisecond
	}
	switch {
	case len(input.Query) > maxMCPSearchQueryLength:
		httpx.WriteInvalidRequest(ctx, w, "query too long", nil)
		return
	case limit < 1 || limit > maxMCPSearchLimit:
		httpx.WriteInvalidRequest(ctx, w, "limit out of range", nil)
		return
	case timeout < 0 || timeout > maxMCPSearchTimeout:
		httpx.WriteInvalidRequest(ctx, w, "timeoutMs out of range", nil)
		return
	case input.Cursor == "" && strings.TrimSpace(input.Query) == "":
		httpx.WriteInvalidRequest(ctx, w, "query is required without a cursor", nil)
		return
	}
	if !p.requireMember(ctx, w, org, credInfo.Subject) {
		return
	}
	if p.fedSearch == nil {
		httpx.WriteNotSupported(ctx, w, "search is not configured")
		return
	}

	var page fedsearch.Page
	if input.Cursor != "" {
		page, err = p.fedSearch.Continue(ctx, credInfo.Subject, input.Cursor, limit, timeout)
		if errors.Is(err, fedsearch.ErrInvalidCursor) || errors.Is(err, fedsearch.ErrNotFound) {
			httpx.WriteError(ctx, w, "InvalidCursor",
				"invalid or expired cursor; search again", http.StatusBadRequest)
			return
		}
	} else {
		var s *fedsearch.Search
		s, err = p.fedSearch.Start(ctx, credInfo.Subject, org, fedsearch.Query{Text: input.Query})
		if err == nil {
			page, err = s.Next(ctx, fedsearch.NextOptions{
				Limit: limit, Timeout: timeout, WaitAll: true,
			})
		}
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, err)
		return
	}
	httpx.WriteJSON(ctx, w, mcpSearchOutput(page))
}

func mcpSearchOutput(page fedsearch.Page) habitat.NetworkHabitatMcpSearchOutput {
	out := habitat.NetworkHabitatMcpSearchOutput{
		Results:  make([]habitat.NetworkHabitatMcpSearchResult, len(page.Results)),
		Backends: make([]habitat.NetworkHabitatMcpSearchBackend, len(page.Backends)),
		Cursor:   page.Cursor,
		Ranking:  string(page.Ranking),
	}
	for i, r := range page.Results {
		out.Results[i] = habitat.NetworkHabitatMcpSearchResult{
			Backend: string(r.Backend), Kind: string(r.Kind), Id: r.ID,
			Title: r.Title, Url: r.URL, Snippet: r.Snippet,
		}
		var record search.RecordData
		if r.Kind == search.ResultKind && json.Unmarshal(r.Data, &record) == nil {
			out.Results[i].Record = &habitat.NetworkHabitatMcpSearchRecord{
				Space: record.Space.String(), Collection: record.Collection.String(),
				Value: record.Value,
			}
		}
	}
	for i, b := range page.Backends {
		out.Backends[i] = habitat.NetworkHabitatMcpSearchBackend{
			Id: string(b.ID), Kind: string(b.Kind), Name: b.Name, Status: string(b.Status),
			Count: int64(b.Count), ElapsedMs: b.Elapsed.Milliseconds(), Cursor: b.Cursor,
		}
		if b.Error != nil {
			out.Backends[i].ErrorCode, out.Backends[i].Error = string(b.Error.Code), b.Error.Message
		}
	}
	return out
}
