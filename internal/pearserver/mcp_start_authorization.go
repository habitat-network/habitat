package pearserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
)

// StartAuthorization implements network.habitat.mcp.startAuthorization.
func (p *PearServer) StartAuthorization(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatMcpStartAuthorizationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "reading request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	if input.Id == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required fields", nil)
		return
	}
	if !p.requireMember(ctx, w, org, credInfo.Subject) {
		return
	}

	sessionToken, err := p.mcpGatewayStore.StartAuthorization(
		ctx, credInfo.Subject, org, syntax.RecordKey(input.Id),
	)
	if errors.Is(err, opensocial.ErrMcpServerNotFound) {
		httpx.WriteError(ctx, w, "NotFound", "mcp server not found", http.StatusNotFound)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, err)
		return
	}

	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatMcpStartAuthorizationOutput{
		SessionToken: sessionToken,
	})
}
