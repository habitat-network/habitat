package pearserver

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func (p *PearServer) ListRepos(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatSpaceListReposParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	_, ok = p.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleReader),
	).Validate(w, r)
	if !ok {
		return
	}
	var since syntax.TID
	// The cursor is a space revision: list only repos written after it.
	if params.Cursor != "" {
		var err error
		since, err = syntax.ParseTID(params.Cursor)
		if err != nil {
			httpx.WriteInvalidRequest(ctx, w, "invalid cursor", err)
			return
		}
	}
	spaceRev, repos, err := p.spacesStore.ListReposSince(r.Context(), spaceURI, since)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list repos: %w", err))
		return
	}
	if httpx.IsComAtprotoRequest(r) {
		views := make([]comatproto.ComAtprotoSpaceListReposRepo, len(repos))
		for i, repo := range repos {
			views[i] = comatproto.ComAtprotoSpaceListReposRepo{
				Did:      repo.DID.String(),
				Hash:     repo.Hash,
				RepoRev:  repo.Rev,
				SpaceRev: repo.SpaceRev.String(),
			}
		}
		httpx.WriteJSON(ctx, w, comatproto.ComAtprotoSpaceListReposOutput{
			Repos:  views,
			Cursor: spaceRev.String(),
		})
		return
	}
	repoViews := make([]habitat.NetworkHabitatSpaceListReposRepo, len(repos))
	for i, r := range repos {
		repoViews[i] = habitat.NetworkHabitatSpaceListReposRepo{
			Did:      r.DID.String(),
			Rev:      r.Rev,
			Hash:     r.Hash,
			RepoRev:  r.Rev,
			SpaceRev: r.SpaceRev.String(),
		}
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceListReposOutput{
		Repos:  repoViews,
		Cursor: spaceRev.String(),
	})
}
