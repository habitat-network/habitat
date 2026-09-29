package pearserver

import (
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func (p *PearServer) GetLatestCommit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatSpaceGetLatestCommitParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid query params", err)
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
	repoDID, ok := httpx.ParseDIDInput(ctx, w, params.Repo, "repo")
	if !ok {
		return
	}

	commit, err := p.spacesStore.RepoHeadCommit(ctx, spaceURI, repoDID)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("repo head commit: %w", err))
		return
	}
	if commit == nil {
		httpx.WriteRepoNotFound(ctx, w, spaces.ErrRepoNotFound)
		return
	}

	signed := commit.ToXRPC()
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceGetLatestCommitOutput{
		Commit: &signed,
	})
}
