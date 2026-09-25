package pearserver

import (
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
)

// ListServers implements network.habitat.mcp.listServers.
func (p *PearServer) ListServers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatMcpListServersParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode query params", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, params.Org, "org")
	if !ok {
		return
	}
	if !p.requireMember(ctx, w, org, credInfo.Subject) {
		return
	}

	servers, err := p.mcpGatewayStore.ListServers(ctx, org, credInfo.Subject)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list mcp servers: %w", err))
		return
	}

	out := make([]habitat.NetworkHabitatMcpListServersServerWithStatus, len(servers))
	for i, server := range servers {
		out[i] = habitat.NetworkHabitatMcpListServersServerWithStatus{
			Server:    mcpServerToAPI(server.Server),
			Connected: server.Connected,
		}
	}

	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatMcpListServersOutput{Servers: out})
}
