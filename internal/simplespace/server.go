package simplespace

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gorilla/schema"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type Server struct {
	mgr       *Manager
	validator authn.RequestValidator
	decoder   *schema.Decoder
}

// Copied as-is from internal/spaces/server.go on main — needs to be adapted
// to this package (Server struct/fields, imports, error vars, etc).

func (s *Server) CreateSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
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
	if input.Did != "" {
		parsedDID, ok := httpx.ParseDIDInput(ctx, w, input.Did, "did")
		if !ok {
			return
		}
		if parsedDID != credInfo.Subject && parsedDID != credInfo.Org.DID() {
			httpx.WriteInvalidRequest(ctx, w, "only caller did or caller org are allowed", nil)
			return
		}
	}

	uri, err := s.mgr.CreateSpace(ctx, credInfo.Org.DID(), credInfo.Subject, spaceType, skey)
	if errors.Is(err, ErrSpaceAlreadyExists) {
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

func (s *Server) AddMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSimplespaceAddMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	_, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, fgastore.RelationSpaceMemberManager),
	).Validate(w, r)
	if !ok {
		return
	}
	memberDID, ok := httpx.ParseDIDInput(ctx, w, input.Did, "did")
	if !ok {
		return
	}
	err := s.mgr.AddMember(r.Context(), spaceURI, memberDID)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if errors.Is(err, spaces.ErrUserAlreadyMember) {
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("add member: %w", err))
		return
	}
}

func (s *Server) RemoveMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSimplespaceRemoveMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	_, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, fgastore.RelationSpaceMemberManager),
	).Validate(w, r)
	if !ok {
		return
	}
	memberDID, ok := httpx.ParseDIDInput(ctx, w, input.Did, "did")
	if !ok {
		return
	}
	err := s.mgr.RemoveMember(r.Context(), spaceURI, memberDID)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if errors.Is(err, spaces.ErrNotAMember) {
		return
	} else if errors.Is(err, ErrCannotRemoveOrg) {
		httpx.WriteInvalidRequest(ctx, w, "cannot remove org", err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("remove member: %w", err))
		return
	}
}

func (s *Server) ListMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatSimplespaceListMembersParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, fgastore.RelationSpaceReader),
	).Validate(w, r)
	if !ok {
		return
	}
	dids, err := s.mgr.ListMembers(ctx, credInfo.Org.DID(), spaceURI)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list members: %w", err))
		return
	}
	members := make([]habitat.NetworkHabitatSimplespaceListMembersMember, len(dids))
	for i, did := range dids {
		members[i] = habitat.NetworkHabitatSimplespaceListMembersMember{Did: did.String()}

	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSimplespaceListMembersOutput{Members: members})
}

func (s *Server) DeleteSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSimplespaceDeleteSpaceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	_, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, fgastore.RelationSpaceOwner),
	).Validate(w, r)
	if !ok {
		return
	}
	err := s.mgr.DeleteSpace(ctx, spaceURI)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(r.Context(), w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("delete space: %w", err))
		return
	}
}
