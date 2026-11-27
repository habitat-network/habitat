package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/telemetry"
)

// RequestJoin implements network.habitat.opensocial.requestJoin.
func (p *PearServer) RequestJoin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatOpensocialRequestJoinInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	trace.SpanFromContext(ctx).SetAttributes(telemetry.Org(org))
	roles, err := p.opensocialStore.RequestJoin(ctx, org, credInfo.Subject)
	if errors.Is(err, opensocial.ErrInviteNotFound) {
		httpx.WriteError(
			ctx, w, "InviteNotFound",
			"the calling user has no pending invite to this community", http.StatusNotFound,
		)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("accept invite: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatOpensocialRequestJoinOutput{Roles: roles})
}
