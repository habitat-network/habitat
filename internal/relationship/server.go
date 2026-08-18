package relationship

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/schema"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Server exposes the network.habitat.relationship.* XRPC endpoints. Writes
// require the manager role and reads require the reader role on the governing
// space, checked via FGA exactly like internal/spaces.
type Server struct {
	store     *Store
	fga       fgastore.Store
	validator authn.RequestValidator
	decoder   *schema.Decoder
}

func NewServer(store *Store, fga fgastore.Store, validator authn.RequestValidator) *Server {
	return &Server{
		store:     store,
		fga:       fga,
		validator: validator,
		decoder:   schema.NewDecoder(),
	}
}

// authorize reports whether the caller holds the given relation on the space,
// using the owner contextual tuple so the org owner always passes.
func (s *Server) authorize(
	ctx context.Context,
	caller *authn.CredentialInfo,
	space habitat_syntax.SpaceURI,
	relation string,
) (bool, error) {
	return s.fga.Check(
		ctx,
		fgastore.MemberUserString(caller.Subject),
		relation,
		fgastore.SpaceObjectKey(space),
		fgastore.OwnerContextualTuple(space),
		fgastore.OrgMemberContextualTuple(caller.Org.DID()),
	)
}

func (s *Server) WriteUserRelation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatRelationshipWriteUserRelationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode request body", err)
		return
	}
	subject, ok := httpx.ParseDIDInput(ctx, w, input.Subject, "subject")
	if !ok {
		return
	}
	object, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space")
	if !ok {
		return
	}
	if _, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(object, fgastore.RelationSpaceMemberManager),
	).Validate(w, r); !ok {
		return
	}
	role, err := ParseRole(input.Relation)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse relation", err)
	}
	// TODO better relationship store interface
	relationSubject, err := parseSubjectParams(subject.String(), role.String())
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse subject", err)
	}
	uri, err := s.store.WriteTuple(ctx, relationSubject, role, object)
	if errors.Is(err, ErrInvalidTuple) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		httpx.WriteInvalidRequest(ctx, w, "invalid tuple", err)
		return
	} else if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("write tuple: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w,
		habitat.NetworkHabitatRelationshipWriteUserRelationOutput{Uri: uri.String()})
}

func (s *Server) DeleteTuple(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatRelationshipDeleteRelationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode request body", err)
		return
	}
	httpx.ParseSpaceURIInput(ctx, w, input.Uri, "space")
	uri, err := habitat_syntax.ParseSpaceRecordURI(input.Uri)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse uri", err)
		return
	}
	if _, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(uri.SpaceURI(), fgastore.RelationSpaceMemberManager),
	).Validate(w, r); !ok {
		return
	}
	err = s.store.DeleteTuple(ctx, uri)
	if errors.Is(err, ErrTupleNotFound) {
		slog.WarnContext(ctx, "tuple not found", "err", err)
		httpx.WriteError(ctx, w, "RelationNotFound", "", http.StatusNotFound)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("delete tuple: %w", err))
		return
	}
}

func (s *Server) ListTuples(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatRelationshipListRelationsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode query params", err)
		return
	}
	space, ok := httpx.ParseSpaceURIInput(r.Context(), w, params.Space, "space uri")
	if !ok {
		return
	}
	if _, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(space, fgastore.RelationSpaceReader),
	).Validate(w, r); !ok {
		return
	}
	filter, err := parseListTuplesFilter(params)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse filter", err)
		return
	}
	tuples, err := s.store.ListTuples(ctx, space, filter)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list tuples: %w", err))
		return
	}
	views := make([]any, len(tuples))
	for i, t := range tuples {
		if t.Subject.Kind() == SubjectKindSpace {
			subject := t.Subject.(SpaceRoleSubject)
			views[i] = habitat.NetworkHabitatRelationshipListRelationsSpaceRelationView{
				Uri:      t.URI.String(),
				Subject:  subject.Space.String(),
				Relation: string(t.Relation),
				Object:   t.Object.String(),
			}
		}
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatRelationshipListRelationsOutput{Relations: views})
}

func (s *Server) Check(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatRelationshipCheckParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode query params", err)
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space")
	if !ok {
		return
	}
	subject, err := parseSubjectParams(params.Subject, params.SubjectRole)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse subject", err)
		return
	}

	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(space, fgastore.RelationSpaceReader),
	).Validate(w, r)
	if !ok {
		return
	}
	allowed, err := s.store.Check(ctx,
		credInfo.Org.DID(),
		subject, Role(params.Relation), space)
	if errors.Is(err, ErrInvalidTuple) {
		httpx.WriteInvalidRequest(ctx, w, "invalid tuple", err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatRelationshipCheckOutput{Allowed: allowed})
}

func (s *Server) ListSubjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatRelationshipListSubjectsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode query params", err)
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space")
	if !ok {
		return
	}
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(space, fgastore.RelationSpaceReader),
	).Validate(w, r)
	if !ok {
		return
	}
	dids, err := s.store.ListSubjects(ctx, credInfo.Org.DID(), space, Role(params.Relation))
	if errors.Is(err, ErrInvalidTuple) {
		httpx.WriteInvalidRequest(ctx, w, "invalid tuple", err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list subjects: %w", err))
		return
	}
	out := make([]string, len(dids))
	for i, did := range dids {
		out[i] = did.String()
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatRelationshipListSubjectsOutput{Dids: out})
}

func (s *Server) ListObjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var params habitat.NetworkHabitatRelationshipListObjectsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode query params", err)
		return
	}
	did, ok := httpx.ParseDIDInput(ctx, w, params.Did, "did")
	if !ok {
		return
	}
	var filterType *syntax.NSID
	if params.Type != "" {
		t, ok := httpx.ParseNSIDInput(ctx, w, params.Type, "type filter")
		if !ok {
			return
		}
		filterType = &t
	}
	spaceURIs, err := s.store.ListObjects(
		ctx,
		credInfo.Org.DID(),
		did,
		Role(params.Relation),
		filterType,
	)
	if errors.Is(err, ErrInvalidTuple) {
		httpx.WriteInvalidRequest(ctx, w, "invalid tuple", err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list objects: %w", err))
		return
	}
	// Only return spaces the caller is allowed to read.
	out := make([]string, 0, len(spaceURIs))
	for _, space := range spaceURIs {
		readable, err := s.authorize(
			ctx,
			credInfo,
			space,
			fgastore.RelationSpaceReader,
		)
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("check read permission: %w", err))
			return
		}
		if readable {
			out = append(out, space.String())
		}
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatRelationshipListObjectsOutput{Spaces: out})
}

// parseListTuplesFilter builds a store filter from the query params, validating
// the optional filter values.
func parseListTuplesFilter(
	params habitat.NetworkHabitatRelationshipListRelationsParams,
) (ListTuplesFilter, error) {
	var filter ListTuplesFilter
	if params.Object != "" {
		object, err := habitat_syntax.ParseSpaceURI(params.Object)
		if err != nil {
			return ListTuplesFilter{}, err
		}
		filter.Object = &object
	}
	if params.SubjectDid != "" {
		did, err := syntax.ParseDID(params.SubjectDid)
		if err != nil {
			return ListTuplesFilter{}, err
		}
		filter.SubjectDID = &did
	}
	switch params.SubjectType {
	case "":
	case string(SubjectKindUser):
		filter.SubjectKind = SubjectKindUser
	case string(SubjectKindSpace):
		filter.SubjectKind = SubjectKindSpace
	default:
		return ListTuplesFilter{}, errors.New("invalid subjectType")
	}
	if params.Relation != "" {
		role := Role(params.Relation)
		filter.Relation = &role
	}
	return filter, nil
}
