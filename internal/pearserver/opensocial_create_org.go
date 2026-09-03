package pearserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
)

// CreateOrg implements network.habitat.opensocial.createOrg.
func (p *PearServer) CreateOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatOpensocialCreateOrgInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	if input.Handle == "" {
		httpx.WriteInvalidRequest(ctx, w, "handle is required", nil)
		return
	}
	org, err := p.opensocialStore.NewOrg(ctx, input.Handle, credInfo.Subject)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("new org: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatOpensocialCreateOrgOutput{Org: org})
}
