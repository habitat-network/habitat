package pearserver

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/search"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

// maxSearchLimit is the largest page searchRecords returns.
const maxSearchLimit = 100

func (p *PearServer) SearchRecords(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if p.searcher == nil {
		httpx.WriteNotSupported(ctx, w, "search is not configured on this server")
		return
	}
	var params habitat.NetworkHabitatSpaceSearchRecordsParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	if params.Q == "" {
		httpx.WriteInvalidRequest(ctx, w, "q is required", nil)
		return
	}
	if params.Limit < 0 || params.Limit > maxSearchLimit {
		httpx.WriteInvalidRequest(
			ctx,
			w,
			fmt.Sprintf("limit must be between 1 and %d", maxSearchLimit),
			nil,
		)
		return
	}
	q := search.Query{
		Text:   params.Q,
		Limit:  int(params.Limit),
		Cursor: params.Cursor,
	}
	for _, c := range params.Collection {
		collection, ok := httpx.ParseNSIDInput(ctx, w, c, "collection filter")
		if !ok {
			return
		}
		q.Collections = append(q.Collections, collection)
	}
	if params.Org != "" {
		org, ok := httpx.ParseDIDInput(ctx, w, params.Org, "org")
		if !ok {
			return
		}
		q.Org = org
	}
	if params.Repo != "" {
		repo, ok := httpx.ParseDIDInput(ctx, w, params.Repo, "repo")
		if !ok {
			return
		}
		q.Repos = []syntax.DID{repo}
	}

	validateOpts := []utils.Opt[authn.EndpointOptions]{
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
	}
	if params.Space != "" {
		space, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
		if !ok {
			return
		}
		// Checks the caller can read the space, as listRecords does.
		validateOpts = append(validateOpts, authn.WithSpace(space, habitat_syntax.SpaceRoleReader))
		q.Spaces = []habitat_syntax.SpaceURI{space}
		if q.Org != "" && q.Org != space.SpaceOwner() {
			httpx.WriteInvalidRequest(ctx, w, "space is not owned by org", nil)
			return
		}
		q.Org = space.SpaceOwner()
	}
	credInfo, ok := p.validator.Request(validateOpts...).Validate(w, r)
	if !ok {
		return
	}
	// A space credential's space implies the org. Anything else searches one
	// org at a time.
	if q.Org == "" && credInfo.Space == "" {
		httpx.WriteInvalidRequest(ctx, w, "org or space is required", nil)
		return
	}

	var page search.Page
	var err error
	if credInfo.Space != "" {
		// A space credential reads only its own space, all of it.
		q.Spaces = []habitat_syntax.SpaceURI{credInfo.Space}
		q.Org = credInfo.Space.SpaceOwner()
		page, err = p.searcher.SearchSpaces(ctx, q)
	} else {
		// The index only matches what the caller may read.
		page, err = p.searcher.Search(ctx, credInfo.Subject, q)
	}
	if errors.Is(err, search.ErrInvalidCursor) {
		httpx.WriteInvalidRequest(ctx, w, "invalid cursor", err)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("search records: %w", err))
		return
	}

	out := habitat.NetworkHabitatSpaceSearchRecordsOutput{
		Cursor:  page.Cursor,
		Records: make([]habitat.NetworkHabitatSpaceSearchRecordsRecord, len(page.Matches)),
	}
	for i, m := range page.Matches {
		out.Records[i] = habitat.NetworkHabitatSpaceSearchRecordsRecord{
			Uri:     m.URI.String(),
			Space:   m.Record.Space.String(),
			Cid:     m.Record.Cid.String(),
			Value:   m.Record.Value,
			Snippet: m.Snippet,
		}
	}
	httpx.WriteJSON(ctx, w, out)
}
