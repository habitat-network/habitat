package pearserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
)

// UpdatePermissions implements community.opensocial.updatePermissions.
func (p *PearServer) UpdatePermissions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.CommunityOpensocialUpdatePermissionsInput
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
	if err := p.opensocialStore.PutPermissions(
		ctx, org, input.Bindings, input.Assignable,
	); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("update permissions: %w", err))
		return
	}
}
