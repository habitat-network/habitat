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
	"github.com/habitat-network/habitat/internal/opensocial"
)

// PutRole implements network.habitat.opensocial.putRole.
func (p *PearServer) PutRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatOpensocialPutRoleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	if input.Role == "" {
		httpx.WriteInvalidRequest(ctx, w, "role is required", nil)
		return
	}
	if input.Name == "" {
		httpx.WriteInvalidRequest(ctx, w, "name is required", nil)
		return
	}
	if !p.requireAction(ctx, w, org, credInfo.Subject, opensocial.ActionCommunityConfigure) {
		return
	}
	err := p.opensocialStore.SetRoleProfile(
		ctx, org, credInfo.Subject, syntax.RecordKey(input.Role), input.Name, input.Description,
	)
	switch {
	case errors.Is(err, opensocial.ErrInvalidRoleID):
		httpx.WriteInvalidRequest(ctx, w, err.Error(), err)
	case errors.Is(err, opensocial.ErrEscalation):
		httpx.WriteUnauthorized(ctx, w, err.Error(), nil)
	case err != nil:
		httpx.WriteServerError(ctx, w, fmt.Errorf("put role: %w", err))
	}
}
