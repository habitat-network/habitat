package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
)

// UpdatePermissions implements network.habitat.opensocial.updatePermissions.
func (p *PearServer) UpdatePermissions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatOpensocialUpdatePermissionsInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	if !p.requireAction(ctx, w, org, credInfo.Subject, opensocial.ActionCommunityConfigure) {
		return
	}
	if err := p.opensocialStore.UpdatePermissions(
		ctx, org, credInfo.Subject, input.Bindings, input.Assignable,
	); err != nil {
		if errors.Is(err, opensocial.ErrEscalation) {
			httpx.WriteUnauthorized(ctx, w, err.Error(), nil)
			return
		}
		httpx.WriteServerError(ctx, w, fmt.Errorf("update permissions: %w", err))
		return
	}
}
