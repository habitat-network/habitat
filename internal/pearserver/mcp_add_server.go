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
	"github.com/habitat-network/habitat/internal/opensocial"
)

// AddServer implements network.habitat.mcp.addServer.
func (p *PearServer) AddServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatMcpAddServerInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "reading request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	if input.Name == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required fields", nil)
		return
	}
	if !p.requireAction(ctx, w, org, credInfo.Subject, opensocial.ActionMcpConfigure) {
		return
	}

	id, sessionToken, err := p.mcpGatewayStore.BeginAddServer(
		ctx, org, credInfo.Subject, input.Name, input.Description,
	)
	if errors.Is(err, mcpgateway.ErrInvalidServerName) ||
		errors.Is(err, mcpgateway.ErrServerNameTaken) {
		httpx.WriteInvalidRequest(ctx, w, "begin add mcp server", err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("begin add mcp server: %w", err))
		return
	}

	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatMcpAddServerOutput{
		Id:           string(id),
		SessionToken: sessionToken,
	})
}
