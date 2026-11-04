package pearserver

import (
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
)

// ListLlmConnections implements network.habitat.llm.listConnections.
func (p *PearServer) ListLlmConnections(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatLlmListConnectionsParams
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

	conns, err := p.llmStore.List(ctx, org)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list llm connections: %w", err))
		return
	}
	out := make([]habitat.NetworkHabitatLlmDefsConnection, len(conns))
	for i, conn := range conns {
		out[i] = llmConnectionToAPI(conn)
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatLlmListConnectionsOutput{Connections: out})
}
