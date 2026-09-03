package pearserver

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

func (p *PearServer) GetRepo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatSpaceGetRepoParams
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
	repoDID, ok := httpx.ParseDIDInput(ctx, w, params.Repo, "repo")
	if !ok {
		return
	}
	commit, blocks, err := p.spacesStore.RepoSnapshot(ctx, spaceURI, repoDID)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("repo snapshot: %w", err))
		return
	}
	if commit == nil {
		httpx.WriteRepoNotFound(ctx, w, spaces.ErrRepoNotFound)
		return
	}

	carBytes, err := spaces.SerializeRepoCAR(*commit, blocks)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("serialize car: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/vnd.ipld.car")
	if _, err := w.Write(carBytes); err != nil {
		utils.LogAndHTTPError(ctx, w, err, "write car", http.StatusInternalServerError)
	}
}
