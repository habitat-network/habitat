package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/simplespace"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// CreateSpace implements network.habitat.simplespace.createSpace.
func (p *PearServer) CreateSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatSimplespaceCreateSpaceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceType, ok := httpx.ParseNSIDInput(ctx, w, input.Type, "space type")
	if !ok {
		return
	}
	var skey habitat_syntax.SpaceKey
	if input.Skey != "" {
		parsedKey, err := habitat_syntax.ParseSkey(input.Skey)
		if err != nil {
			httpx.WriteInvalidRequest(ctx, w, "invalid skey", err)
			return
		}
		skey = parsedKey
	}

	// Defaults to the caller's own OAuth-session org (internal/org
	// membership) when there is one, exactly as before this handler also
	// accepted an explicit opensocial org below — a bare createSpace call
	// with no did param still means "create in my org's namespace".
	authority := credInfo.Subject
	if credInfo.Org != nil {
		authority = credInfo.Org.DID()
	}
	if input.Did != "" {
		parsedDID, ok := httpx.ParseDIDInput(ctx, w, input.Did, "did")
		if !ok {
			return
		}
		callerOrg := credInfo.Org != nil && parsedDID == credInfo.Org.DID()
		if parsedDID != credInfo.Subject && !callerOrg {
			// Not the caller's own DID or their own OAuth-session org — the
			// remaining legitimate case is acting on behalf of an opensocial
			// org the caller genuinely belongs to, the same authorization
			// community.opensocial.createSpace itself uses (also reached via
			// Atproto-Proxy + service-auth). This is how chalk creates an
			// org-mode doc space.
			if !p.requireMember(ctx, w, parsedDID, credInfo.Subject) {
				return
			}
		}
		authority = parsedDID
	}

	uri, err := p.simpleStore.CreateSpace(ctx, authority, credInfo.Subject, spaceType, skey)
	if errors.Is(err, simplespace.ErrSpaceAlreadyExists) {
		httpx.WriteError(ctx, w, "SpaceAlreadyExists", "" /* msg */, http.StatusBadRequest)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("create space: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSimplespaceCreateSpaceOutput{
		Uri: uri.String(),
	})
}
