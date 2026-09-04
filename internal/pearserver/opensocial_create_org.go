package pearserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
)

// handleLabelRegex matches a single handle label per the atproto handle
// grammar (github.com/bluesky-social/indigo/atproto/syntax.ParseHandle),
// minus the "." and repeated-label parts, since org handles are a single
// label (e.g. "acme"), not a full dotted handle.
var handleLabelRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// CreateOrg implements network.habitat.opensocial.createOrg.
func (p *PearServer) CreateOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatOpensocialCreateOrgInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	if !parseHandle(ctx, w, input.Handle) {
		return
	}
	org, err := p.opensocialStore.NewOrg(ctx, input.Handle, credInfo.Subject)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("new org: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatOpensocialCreateOrgOutput{Org: org})
}

// parseHandle validates that handleStr is a single label suitable for use as
// the org's subdomain (e.g. "acme"), not a full dotted handle.
func parseHandle(ctx context.Context, w http.ResponseWriter, handleStr string) bool {
	if !handleLabelRegex.MatchString(handleStr) {
		httpx.WriteInvalidRequest(ctx, w, fmt.Sprintf("invalid handle: %s", handleStr), nil)
		return false
	}
	return true
}
