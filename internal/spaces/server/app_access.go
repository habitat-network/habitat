package spaces_server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// appAccessRkey deterministically derives the network.habitat.space.appAccess
// record key for a client_id: its base64url (no padding) encoding, so the
// grant for a given client is directly addressable without a lookup index.
func appAccessRkey(clientID string) (syntax.RecordKey, error) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(clientID))
	rkey, err := syntax.ParseRecordKey(encoded)
	if err != nil {
		return "", fmt.Errorf("client id too long to address as a record key: %w", err)
	}
	return rkey, nil
}

// validateClientID reports whether clientID is well-formed enough to store:
// an absolute URL, matching what getSpaceCredential's attestation
// verification expects a client_id to look like.
func validateClientID(clientID string) error {
	u, err := url.Parse(clientID)
	if err != nil || !u.IsAbs() {
		return fmt.Errorf("client id must be an absolute URL")
	}
	return nil
}

func (s *Server) AddAppAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceAddAppAccessInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	if _, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleManager),
	).Validate(w, r); !ok {
		return
	}
	if err := validateClientID(input.ClientId); err != nil {
		httpx.WriteError(ctx, w, "InvalidClientId", err.Error(), http.StatusBadRequest)
		return
	}
	rkey, err := appAccessRkey(input.ClientId)
	if err != nil {
		httpx.WriteError(ctx, w, "InvalidClientId", err.Error(), http.StatusBadRequest)
		return
	}
	recordBytes, err := spaces.MarshalRecord(habitat.NetworkHabitatSpaceAppAccess{})
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("marshal app access record: %w", err))
		return
	}
	recordURI, _, err := s.store.PutRecord(
		ctx, spaceURI, spaceURI.SpaceOwner(), habitat_syntax.AppAccessCollection, rkey, recordBytes,
	)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("put app access record: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceAddAppAccessOutput{Uri: recordURI.String()})
}

func (s *Server) RemoveAppAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceRemoveAppAccessInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	if _, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleManager),
	).Validate(w, r); !ok {
		return
	}
	rkey, err := appAccessRkey(input.ClientId)
	if err != nil {
		httpx.WriteError(ctx, w, "InvalidClientId", err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteRecord(
		ctx, spaceURI, spaceURI.SpaceOwner(), habitat_syntax.AppAccessCollection, string(rkey),
	); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("delete app access record: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceRemoveAppAccessOutput{})
}
