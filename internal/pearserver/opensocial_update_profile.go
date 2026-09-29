package pearserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
)

// UpdateProfile implements community.opensocial.updateProfile.
func (p *PearServer) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.CommunityOpensocialUpdateProfileInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	if input.Name == "" {
		httpx.WriteInvalidRequest(ctx, w, "name is required", nil)
		return
	}
	if !p.requireAdmin(ctx, w, org, credInfo.Subject) {
		return
	}
	if err := p.opensocialStore.UpdateProfile(
		ctx, org, input.Name, input.Description, input.JoinPolicy,
	); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("update profile: %w", err))
		return
	}
}
