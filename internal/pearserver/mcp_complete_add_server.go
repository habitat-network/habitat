package pearserver

import (
	"encoding/json"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
)

// CompleteAddServer implements network.habitat.mcp.completeAddServer.
func (p *PearServer) CompleteAddServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatMcpCompleteAddServerInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "reading request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	if input.Id == "" || input.Name == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required fields", nil)
		return
	}
	if !p.requireAction(ctx, w, org, credInfo.Subject, opensocial.ActionMcpConfigure) {
		return
	}

	server, err := p.mcpGatewayStore.CompleteAddServer(
		ctx, org, credInfo.Subject, syntax.RecordKey(input.Id), input.Name, input.Description,
	)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "complete add mcp server", err)
		return
	}

	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatMcpCompleteAddServerOutput{
		Server: mcpServerToAPI(server),
	})
}
