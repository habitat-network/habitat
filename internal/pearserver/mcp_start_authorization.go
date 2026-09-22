package pearserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/mcpgateway"
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
	if input.Id == "" || input.RedirectUri == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required fields", nil)
		return
	}
	if !p.requireMember(ctx, w, org, credInfo.Subject) {
		return
	}

	authorizationURL, err := p.mcpGatewayStore.StartAuthorization(
		ctx, credInfo.Subject, org, mcpgateway.ServerID(input.Id), input.RedirectUri,
	)
	if errors.Is(err, mcpgateway.ErrServerNotFound) {
		httpx.WriteError(ctx, w, "NotFound", "mcp server not found", http.StatusNotFound)
		return
	} else if errors.Is(err, mcpgateway.ErrNotOAuthServer) {
		httpx.WriteInvalidRequest(ctx, w, "mcp server does not require authorization", err)
		return
	} else if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "start mcp authorization", err)
		return
	}

	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatMcpStartAuthorizationOutput{
		AuthorizationUrl: authorizationURL,
	})
}
