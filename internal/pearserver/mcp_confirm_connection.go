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

// ConfirmConnection implements network.habitat.mcp.confirmConnection.
func (p *PearServer) ConfirmConnection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatMcpConfirmConnectionInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "reading request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	if input.Id == "" || input.ConnectionId == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required fields", nil)
		return
	}
	if !p.requireMember(ctx, w, org, credInfo.Subject) {
		return
	}

	err := p.mcpGatewayStore.ConfirmConnection(
		ctx, credInfo.Subject, org, mcpgateway.ServerID(input.Id), input.ConnectionId,
	)
	if errors.Is(err, mcpgateway.ErrServerNotFound) {
		httpx.WriteError(ctx, w, "NotFound", "mcp server not found", http.StatusNotFound)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, err)
		return
	}
}
