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
	"github.com/habitat-network/habitat/internal/relationship"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
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
	role, err := relationship.ParseRole(input.Relation)
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
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
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
		utils.LogAndHTTPError(r.Context(), w, err, "list tuples", http.StatusInternalServerError)
		httpx.WriteServerError(ctx, w, fmt.Errorf("list tuples: %w", err))
		return
	}
	views := make([]any, len(tuples))
	for i, t := range tuples {
		if t.Subject.Kind() == SubjectKindSpace {
			views[i] = habitat.NetworkHabitatRelationshipListRelationsSpaceRelationView{
				Uri:      t.URI.String(),
				Subject:  t.Subject.toInterface(),
				Relation: string(t.Relation),
				Object: habitat.NetworkHabitatRelationshipDefsSpaceObject{
					Space: t.Object.String(),
				},
			}
		}
	}

	s.writeJSON(w, r, habitat.NetworkHabitatRelationshipListTuplesOutput{Tuples: views})
}

func (s *Server) Check(w http.ResponseWriter, r *http.Request) {
	var params habitat.NetworkHabitatRelationshipCheckParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}

	space, ok := httpx.ParseSpaceURIInput(r.Context(), w, params.Space, "space uri")
	if !ok {
		return
	}

	subject, err := parseSubjectParams(params.Subject, params.SubjectRole)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(space, fgastore.RelationSpaceReader),
	).Validate(w, r)
	if !ok {
		return
	}

	allowed, err := s.store.Check(r.Context(),
		credInfo.Org.DID(),
		subject, Role(params.Relation), space)
	if errors.Is(err, ErrInvalidTuple) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "check", http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, r, habitat.NetworkHabitatRelationshipCheckOutput{Allowed: allowed})
}

func (s *Server) ListSubjects(w http.ResponseWriter, r *http.Request) {
	var params habitat.NetworkHabitatRelationshipListSubjectsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}

	space, ok := httpx.ParseSpaceURIInput(r.Context(), w, params.Space, "space uri")
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

	dids, err := s.store.ListSubjects(r.Context(), credInfo.Org.DID(), space, Role(params.Relation))
	if errors.Is(err, ErrInvalidTuple) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "list subjects", http.StatusInternalServerError)
		return
	}

	out := make([]string, len(dids))
	for i, did := range dids {
		out[i] = did.String()
	}

	s.writeJSON(w, r, habitat.NetworkHabitatRelationshipListSubjectsOutput{Dids: out})
}

func (s *Server) ListObjects(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatRelationshipListObjectsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}

	did, ok := httpx.ParseDIDInput(r.Context(), w, params.Did, "did")
	if !ok {
		return
	}

	var filterType *syntax.NSID
	if params.Type != "" {
		t, ok := httpx.ParseNSIDInput(r.Context(), w, params.Type, "type filter")
		if !ok {
			return
		}
		filterType = &t
	}

	spaceURIs, err := s.store.ListObjects(
		r.Context(),
		credInfo.Org.DID(),
		did,
		Role(params.Relation),
		filterType,
	)
	if errors.Is(err, ErrInvalidTuple) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "list objects", http.StatusInternalServerError)
		return
	}

	// Only return spaces the caller is allowed to read.
	out := make([]string, 0, len(spaceURIs))
	for _, space := range spaceURIs {
		readable, err := s.authorize(
			r.Context(),
			credInfo,
			space,
			fgastore.RelationSpaceReader,
		)
		if err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"check read permission",
				http.StatusInternalServerError,
			)
			return
		}
		if readable {
			out = append(out, space.String())
		}
	}

	s.writeJSON(w, r, habitat.NetworkHabitatRelationshipListObjectsOutput{Spaces: out})
}

// parseListTuplesFilter builds a store filter from the query params, validating
// the optional filter values.
func parseListTuplesFilter(
	params habitat.NetworkHabitatRelationshipListTuplesParams,
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

func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, body any) {
	httpx.WriteJSON(r.Context(), w, body)
}
