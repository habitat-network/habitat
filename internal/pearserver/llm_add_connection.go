package pearserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/llmgateway"
)

// AddLlmConnection implements network.habitat.llm.addConnection.
func (p *PearServer) AddLlmConnection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatLlmAddConnectionInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "reading request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	if !p.requireAction(ctx, w, org, credInfo.Subject, llmConfigureAction) {
		return
	}

	conn, err := p.llmStore.Add(ctx, org, llmgateway.NewConnection{
		Name:        input.Name,
		Description: input.Description,
		Provider:    llmgateway.Provider(input.Provider),
		Model:       input.Model,
		BaseURL:     input.BaseUrl,
		APIKey:      input.ApiKey,
	})
	if llmgateway.IsConfigError(err) {
		httpx.WriteInvalidRequest(ctx, w, "add llm connection", err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("add llm connection: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatLlmAddConnectionOutput{
		Connection: llmConnectionToAPI(conn),
	})
}
