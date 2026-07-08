package relationship

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/schema"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

// Server exposes the network.habitat.relationship.* XRPC endpoints. Writes
// require the manager role and reads require the reader role on the governing
// space, checked via FGA exactly like internal/spaces.
type Server struct {
	store       *Store
	fga         fgastore.Store
	oauth       authn.Method
	serviceAuth authn.Method
	decoder     *schema.Decoder
}

func NewServer(store *Store, fga fgastore.Store, oauth, serviceAuth authn.Method) *Server {
	return &Server{
		store:       store,
		fga:         fga,
		oauth:       oauth,
		serviceAuth: serviceAuth,
		decoder:     schema.NewDecoder(),
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
		ownerContextualTuple(space),
		fgastore.OrgMemberContextualTuple(caller.Org.DID()),
	)
}

// requireRole authorizes the caller or writes the appropriate error response,
// returning false if the request should not proceed.
func (s *Server) requireRole(
	w http.ResponseWriter,
	r *http.Request,
	caller *authn.CredentialInfo,
	space habitat_syntax.SpaceURI,
	relation string,
) bool {
	authorized, err := s.authorize(r.Context(), caller, space, relation)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"check permission",
			http.StatusInternalServerError,
		)
		return false
	}
	if !authorized {
		http.Error(w, "not authorized", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) WriteTuple(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
		authn.WithSupportedCredentials(authn.UserCredential, authn.OrgCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatRelationshipWriteTupleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode request body", http.StatusBadRequest)
		return
	}

	subject, err := parseSubjectInput(input.Subject)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	object, err := habitat_syntax.ParseSpaceURI(input.Object.Space)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse object space", http.StatusBadRequest)
		return
	}

	if !s.requireRole(
		w,
		r,
		credInfo,
		object,
		fgastore.RelationSpaceMemberManager,
	) {
		return
	}

	uri, err := s.store.WriteTuple(r.Context(), subject, Role(input.Relation), object)
	if errors.Is(err, ErrInvalidTuple) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if errors.Is(err, spaces.ErrSpaceNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "write tuple", http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, r, habitat.NetworkHabitatRelationshipWriteTupleOutput{Uri: uri.String()})
}

func (s *Server) DeleteTuple(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
		authn.WithSupportedCredentials(authn.UserCredential, authn.OrgCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatRelationshipDeleteTupleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode request body", http.StatusBadRequest)
		return
	}

	uri := habitat_syntax.SpaceRecordURI(input.Uri)
	space := uri.SpaceURI()
	if space == "" {
		http.Error(w, "invalid tuple uri", http.StatusBadRequest)
		return
	}

	if !s.requireRole(
		w,
		r,
		credInfo,
		space,
		fgastore.RelationSpaceMemberManager,
	) {
		return
	}

	err := s.store.DeleteTuple(r.Context(), uri)
	if errors.Is(err, ErrTupleNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "delete tuple", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) ListTuples(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
		authn.WithSupportedCredentials(authn.UserCredential, authn.OrgCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatRelationshipListTuplesParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}

	space, err := habitat_syntax.ParseSpaceURI(params.Space)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	if !s.requireRole(
		w,
		r,
		credInfo,
		space,
		fgastore.RelationSpaceReader,
	) {
		return
	}

	filter, err := parseListTuplesFilter(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tuples, err := s.store.ListTuples(r.Context(), space, filter)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "list tuples", http.StatusInternalServerError)
		return
	}

	views := make([]habitat.NetworkHabitatRelationshipListTuplesTupleView, len(tuples))
	for i, t := range tuples {
		views[i] = habitat.NetworkHabitatRelationshipListTuplesTupleView{
			Uri:      t.URI.String(),
			Subject:  t.Subject.toInterface(),
			Relation: string(t.Relation),
			Object:   habitat.NetworkHabitatRelationshipDefsSpaceObject{Space: t.Object.String()},
		}
	}

	s.writeJSON(w, r, habitat.NetworkHabitatRelationshipListTuplesOutput{Tuples: views})
}

func (s *Server) Check(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
		authn.WithSupportedCredentials(authn.UserCredential, authn.OrgCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatRelationshipCheckParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}

	space, err := habitat_syntax.ParseSpaceURI(params.Space)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	subject, err := parseSubjectParams(params.Subject, params.SubjectRole)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if !s.requireRole(
		w,
		r,
		credInfo,
		space,
		fgastore.RelationSpaceReader,
	) {
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
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
		authn.WithSupportedCredentials(authn.UserCredential, authn.OrgCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatRelationshipListSubjectsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}

	space, err := habitat_syntax.ParseSpaceURI(params.Space)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	if !s.requireRole(
		w,
		r,
		credInfo,
		space,
		fgastore.RelationSpaceReader,
	) {
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
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
		authn.WithSupportedCredentials(authn.UserCredential, authn.OrgCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatRelationshipListObjectsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}

	did, err := syntax.ParseDID(params.Did)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse did", http.StatusBadRequest)
		return
	}

	var filterType *syntax.NSID
	if params.Type != "" {
		t, err := syntax.ParseNSID(params.Type)
		if err != nil {
			utils.LogAndHTTPError(r.Context(), w, err, "parse type filter", http.StatusBadRequest)
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
