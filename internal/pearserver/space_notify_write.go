package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// NotifyWrite implements the space-host side of network.habitat.space.notifyWrite:
// a repo host whose PDS holds a space member's records directly (rather than
// through this host's PutRecord) reports that the repo advanced to a new
// revision, so ListRepos can include it. This host records the reported rev
// and commit digest and forwards the notification to any syncers registered
// for the space, exactly as a local write does.
func (p *PearServer) NotifyWrite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceNotifyWriteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleWriter),
	).Validate(w, r)
	if !ok {
		return
	}
	repo, ok := httpx.ParseDIDInput(ctx, w, input.Repo, "repo")
	if !ok {
		return
	}
	if credInfo.Subject != repo {
		httpx.WriteInvalidRequest(ctx, w, "can't notify for other repo", fmt.Errorf("wrong repo"))
		return
	}
	if input.Rev == "" {
		httpx.WriteInvalidRequest(ctx, w, "rev is required", nil)
		return
	}

	err := p.spacesStore.RegisterRemoteWrite(
		ctx, spaceURI, repo, syntax.TID(input.Rev), []byte(input.Hash),
	)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("register remote write: %w", err))
		return
	}
}
