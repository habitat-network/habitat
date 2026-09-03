package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
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

	collection := habitat_syntax.AppAccessCollection
	grants, err := p.spacesStore.ListRecords(ctx, spaceURI, spaceURI.SpaceOwner(), &collection)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list app access grants: %w", err))
		return
	}
	allowListed := len(grants) > 0

	if input.ClientAttestation == "" {
		if allowListed {
			httpx.WriteError(
				ctx, w, "InvalidClientAttestation",
				"space requires a client attestation", http.StatusBadRequest,
			)
			return
		}
	} else {
		clientID, err := spaces.VerifyAttestation(
			ctx, p.clientMeta, input.ClientAttestation, spaceURI.SpaceOwner(),
		)
		if errors.Is(err, spaces.ErrInvalidAttestation) {
			httpx.WriteError(ctx, w, "InvalidClientAttestation", err.Error(), http.StatusBadRequest)
			return
		} else if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("verify attestation: %w", err))
			return
		}
		if allowListed {
			rkey, err := appAccessRkey(clientID)
			if err != nil {
				httpx.WriteError(
					ctx, w, "InvalidClientAttestation", err.Error(), http.StatusBadRequest,
				)
				return
			}
			if _, err := p.spacesStore.GetRecord(
				ctx, spaceURI, spaceURI.SpaceOwner(), habitat_syntax.AppAccessCollection, rkey,
			); errors.Is(err, spaces.ErrRecordNotFound) {
				httpx.WriteError(ctx, w, "AppNotAuthorized", "", http.StatusBadRequest)
				return
			} else if err != nil {
				httpx.WriteServerError(ctx, w, fmt.Errorf("check app access grant: %w", err))
				return
			}
		}
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
