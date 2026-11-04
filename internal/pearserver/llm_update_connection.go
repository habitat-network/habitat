package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/llmgateway"
)

// UpdateLlmConnection implements network.habitat.llm.updateConnection.
func (p *PearServer) UpdateLlmConnection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatLlmUpdateConnectionInput
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
	if !p.requireAction(ctx, w, org, credInfo.Subject, llmConfigureAction) {
		return
	}

	var update llmgateway.ConnectionUpdate
	if input.Description != "" {
		update.Description = &input.Description
	}
	if input.Model != "" {
		update.Model = &input.Model
	}
	if input.BaseUrl != "" {
		update.BaseURL = &input.BaseUrl
	}
	if input.ApiKey != "" {
		update.APIKey = &input.ApiKey
	} else if input.ClearApiKey {
		empty := ""
		update.APIKey = &empty
	}

	conn, err := p.llmStore.Update(ctx, org, syntax.RecordKey(input.Id), update)
	if errors.Is(err, llmgateway.ErrConnectionNotFound) {
		httpx.WriteError(ctx, w, "NotFound", "llm connection not found", http.StatusNotFound)
		return
	} else if llmgateway.IsConfigError(err) {
		httpx.WriteInvalidRequest(ctx, w, "update llm connection", err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("update llm connection: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatLlmUpdateConnectionOutput{
		Connection: llmConnectionToAPI(conn),
	})
}
