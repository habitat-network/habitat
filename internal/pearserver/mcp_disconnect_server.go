package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/mcpgateway"
)

// DisconnectServer implements network.habitat.mcp.disconnectServer.
func (p *PearServer) DisconnectServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatMcpDisconnectServerInput
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

	err := p.mcpGatewayStore.DisconnectServer(ctx, credInfo.Subject, mcpgateway.ServerID(input.Id))
	if errors.Is(err, mcpgateway.ErrCredentialNotFound) {
		httpx.WriteError(ctx, w, "NotFound", "not connected to mcp server", http.StatusNotFound)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("disconnect from mcp server: %w", err))
		return
	}
}
