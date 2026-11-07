package pearserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/mcpsearch"
)

// McpSearch implements network.habitat.mcp.search.
func (p *PearServer) McpSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatMcpSearchInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "reading request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	if len(input.Query) > maxMCPSearchQueryLength || len(input.Servers) > maxMCPSearchServers {
		httpx.WriteInvalidRequest(ctx, w, "query or servers too long", nil)
		return
	}
	if !p.requireMember(ctx, w, org, credInfo.Subject) {
		return
	}

	res, err := p.mcpSearcher.Search(ctx, credInfo.Subject, org, input.Servers, mcpsearch.Query{
		Text: input.Query, Limit: int(input.Limit),
	})
	if errors.Is(err, mcpsearch.ErrEmptyQuery) || errors.Is(err, mcpsearch.ErrInvalidLimit) ||
		errors.Is(err, mcpsearch.ErrInvalidServerURL) {
		httpx.WriteInvalidRequest(ctx, w, "search mcp servers", err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, err)
		return
	}

	out := habitat.NetworkHabitatMcpSearchOutput{
		Items:   make([]habitat.NetworkHabitatMcpSearchItem, len(res.Items)),
		Sources: make([]habitat.NetworkHabitatMcpSearchSource, len(res.Sources)),
	}
	for i, item := range res.Items {
		out.Items[i] = habitat.NetworkHabitatMcpSearchItem{
			ServerUrl: item.Source.ServerURL,
			ServerId:  string(item.Source.ServerName),
			Provider:  string(item.Source.Provider),
			Title:     item.Title,
			Url:       item.URL,
			Snippet:   item.Snippet,
		}
	}
	for i, source := range res.Sources {
		out.Sources[i] = habitat.NetworkHabitatMcpSearchSource{
			ServerUrl: source.ServerURL,
			ServerId:  string(source.ServerName),
			Provider:  string(source.Provider),
			Status:    string(source.Status),
			Error:     source.Error,
			Count:     int64(source.Count),
		}
	}
	httpx.WriteJSON(ctx, w, out)
}

// maxMCPSearchQueryLength and maxMCPSearchServers match the
// network.habitat.mcp.search lexicon's limits.
const (
	maxMCPSearchQueryLength = 1000
	maxMCPSearchServers     = 50
)
