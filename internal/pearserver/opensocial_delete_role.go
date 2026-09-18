package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
)

// DeleteRole implements community.opensocial.deleteRole.
func (p *PearServer) DeleteRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.CommunityOpensocialDeleteRoleInput
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
	if !p.requireAction(ctx, w, org, credInfo.Subject, opensocial.ActionCommunityConfigure) {
		return
	}
	err := p.opensocialStore.DeleteRole(ctx, org, syntax.RecordKey(input.Role))
	if errors.Is(err, opensocial.ErrRoleNotFound) {
		httpx.WriteError(ctx, w, "RoleNotFound", "", http.StatusBadRequest)
		return
	}
	if errors.Is(err, opensocial.ErrCannotDeleteBuiltinRole) {
		httpx.WriteError(ctx, w, "CannotDeleteBuiltinRole", "", http.StatusBadRequest)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("delete role: %w", err))
		return
	}
}
