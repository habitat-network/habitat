package pearserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/clientmetadata"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

func (p *PearServer) GetSpaceCredential(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceGetSpaceCredentialInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	if _, ok = p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodDelegationToken),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleReader),
	).Validate(w, r); !ok {
		return
	}

	if !p.verifyClientAttestation(ctx, w, spaceURI, input.ClientAttestation) {
		return
	}

	kid := "#atproto"
	privKey, err := p.hive.PrivateKeyForDID(ctx, spaceURI.SpaceOwner())
	if errors.Is(err, identity.ErrDIDNotFound) {
		privKey = p.hostKey
		kid = "#atproto_space"
	} else if err != nil {
		httpx.WriteSpaceNotFound(ctx, w, fmt.Errorf("failed to get host private key: %w", err))
		return
	}
	token, err := utils.SpaceCredential(privKey, kid, spaceURI)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to sign token: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: token})
}

func (p *PearServer) verifyClientAttestation(
	ctx context.Context,
	w http.ResponseWriter,
	spaceURI habitat_syntax.SpaceURI,
	attestation string,
) bool {
	collection := habitat_syntax.AppAccessCollection
	grants, err := p.spacesStore.ListRecords(ctx, spaceURI, spaceURI.SpaceOwner(), &collection)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list app access grants: %w", err))
		return false
	}
	if len(grants) > 0 {
		return true
	}
	if attestation == "" {
		httpx.WriteInvalidClientAttestation(ctx, w, "space requires a client attestation", nil)
		return false
	}
	clientID, err := clientmetadata.VerifyAttestation(
		ctx, p.clientMeta, attestation, spaceURI.SpaceOwner(),
	)
	if errors.Is(err, clientmetadata.ErrInvalidAttestation) {
		httpx.WriteInvalidClientAttestation(ctx, w, err.Error(), err)
		return false
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("verify attestation: %w", err))
		return false
	}
	rkey, err := habitat_syntax.AppAccessRkey(clientID)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get app access rkey: %w", err))
		return false
	}
	if _, err := p.spacesStore.GetRecord(
		ctx, spaceURI, spaceURI.SpaceOwner(), habitat_syntax.AppAccessCollection, rkey,
	); errors.Is(err, spaces.ErrRecordNotFound) {
		httpx.WriteError(ctx, w, "AppNotAuthorized", "", http.StatusBadRequest)
		return false
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check app access grant: %w", err))
		return false
	}

	return true
}
