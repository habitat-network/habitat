package pearserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
)

// AddManualServer implements network.habitat.mcp.addManualServer.
func (p *PearServer) AddManualServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatMcpAddManualServerInput
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
	if !p.requireAction(ctx, w, org, credInfo.Subject, opensocial.ActionMcpConfigure) {
		return
	}

	server, err := p.mcpGatewayStore.AddManualServer(
		ctx, org, input.Name, input.Description, input.Url, mcpHeadersFromAPI(input.Headers),
	)
	if isMcpConfigError(err) {
		httpx.WriteInvalidRequest(ctx, w, "add manual mcp server", err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("add manual mcp server: %w", err))
		return
	}

	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatMcpAddManualServerOutput{
		Server: mcpServerToAPI(server),
	})
}
