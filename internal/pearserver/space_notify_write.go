package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// maxRevClockSkew is how far ahead of this host's clock a notifyWrite rev may
// be before it is rejected as a future timestamp.
const maxRevClockSkew = 5 * time.Minute

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
	rev, err := syntax.ParseTID(input.Rev)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "rev must be a TID", err)
		return
	}
	// A rev must not be a future timestamp beyond a short clock skew window,
	// per the spaces proposal; one that is would win every later comparison.
	if rev.Time().After(time.Now().Add(maxRevClockSkew)) {
		httpx.WriteInvalidRequest(ctx, w, "rev is in the future", nil)
		return
	}

	err = p.spacesStore.RegisterRemoteWrite(
		ctx, spaceURI, repo, rev, []byte(input.Data),
	)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("register remote write: %w", err))
		return
	}
}
