package pearserver

import (
	"encoding/json"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
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
	if input.Name == "" || input.Url == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required fields", nil)
		return
	}
	if !p.requireAction(ctx, w, org, credInfo.Subject, opensocial.ActionCommunityConfigure) {
		return
	}

	server, err := p.mcpGatewayStore.AddServer(ctx, org, input.Name, input.Url, input.Description)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "add mcp server", err)
		return
	}

	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatMcpAddServerOutput{
		Server: mcpServerToAPI(server),
	})
}
