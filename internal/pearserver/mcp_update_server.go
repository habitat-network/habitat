package pearserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/opensocial"
)

// UpdateServer implements network.habitat.mcp.updateServer.
func (p *PearServer) UpdateServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatMcpUpdateServerInput
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
	if !p.requireAction(ctx, w, org, credInfo.Subject, opensocial.ActionCommunityConfigure) {
		return
	}

	var name, url, description *string
	if input.Name != "" {
		name = &input.Name
	}
	if input.Url != "" {
		url = &input.Url
	}
	if input.Description != "" {
		description = &input.Description
	}

	server, err := p.mcpGatewayStore.UpdateServer(
		ctx,
		org,
		mcpgateway.ServerID(input.Id),
		name,
		url,
		description,
	)
	if errors.Is(err, mcpgateway.ErrServerNotFound) {
		httpx.WriteError(ctx, w, "NotFound", "mcp server not found", http.StatusNotFound)
		return
	} else if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "update mcp server", err)
		return
	}

	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatMcpUpdateServerOutput{
		Server: mcpServerToAPI(server),
	})
}
