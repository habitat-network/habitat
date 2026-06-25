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
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

// Server exposes the relationship store over XRPC.
type Server struct {
	store       Store
	fga         fgastore.Store
	oauth       authn.Method
	serviceAuth authn.Method
	decoder     *schema.Decoder
}

func NewServer(
	store Store,
	fga fgastore.Store,
	oauth authn.Method,
	serviceAuth authn.Method,
) *Server {
	return &Server{
		store:       store,
		fga:         fga,
		oauth:       oauth,
		serviceAuth: serviceAuth,
		decoder:     schema.NewDecoder(),
	}
}

// authorize checks the caller has the given relation on the space, using the
// owner contextual tuple so space owners always pass.
func (s *Server) authorize(
	ctx context.Context,
	caller syntax.DID,
	space habitat_syntax.SpaceURI,
	relation string,
) (bool, error) {
	return s.fga.Check(
		ctx,
		fgastore.MemberUserString(caller),
		relation,
		fgastore.SpaceObjectKey(space),
		ownerContextualTuple(space),
	)
}

// requireManager authorizes the caller as a manager of the space, writing the
// appropriate HTTP error and returning false if not.
func (s *Server) requireManager(
	w http.ResponseWriter,
	r *http.Request,
	caller syntax.DID,
	space habitat_syntax.SpaceURI,
) bool {
	return s.requireRole(w, r, caller, space, fgastore.RelationSpaceMemberManager)
}

func (s *Server) requireReader(
	w http.ResponseWriter,
	r *http.Request,
	caller syntax.DID,
	space habitat_syntax.SpaceURI,
) bool {
	return s.requireRole(w, r, caller, space, fgastore.RelationSpaceReader)
}

func (s *Server) requireRole(
	w http.ResponseWriter,
	r *http.Request,
	caller syntax.DID,
	space habitat_syntax.SpaceURI,
	relation string,
) bool {
	ok, err := s.authorize(r.Context(), caller, space, relation)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "authorize", http.StatusInternalServerError)
		return false
	}
	if !ok {
		http.Error(w, "not authorized", http.StatusForbidden)
		return false
	}
	return true
}

// writeError maps store sentinels to HTTP responses.
func writeError(ctx context.Context, w http.ResponseWriter, err error, msg string) {
	switch {
	case errors.Is(err, ErrInvalidTuple):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrTupleNotFound), errors.Is(err, ErrGroupNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		utils.LogAndHTTPError(ctx, w, err, msg, http.StatusInternalServerError)
	}
}

func encodeJSON(ctx context.Context, w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		utils.LogAndHTTPError(ctx, w, err, "encode response", http.StatusInternalServerError)
	}
}

func (s *Server) WriteTuple(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatRelationshipWriteTupleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode body", http.StatusBadRequest)
		return
	}
	subject, err := ParseSubject(input.Subject)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	object, err := ParseObject(input.Object)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	space, err := governingSpace(object)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.requireManager(w, r, caller, space) {
		return
	}
	uri, err := s.store.WriteTuple(r.Context(), subject, Role(input.Relation), object)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		http.Error(w, "space not found", http.StatusNotFound)
		return
	} else if err != nil {
		writeError(r.Context(), w, err, "write tuple")
		return
	}
	encodeJSON(
		r.Context(),
		w,
		habitat.NetworkHabitatRelationshipWriteTupleOutput{Uri: uri.String()},
	)
}

func (s *Server) DeleteTuple(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatRelationshipDeleteTupleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode body", http.StatusBadRequest)
		return
	}
	_, space, _, _, _, err := habitat_syntax.ParseSpaceRecordURI(input.Uri)
	if err != nil {
		http.Error(w, "invalid tuple uri", http.StatusBadRequest)
		return
	}
	if !s.requireManager(w, r, caller, space) {
		return
	}
	tupleURI := habitat_syntax.SpaceRecordURI(input.Uri)
	if err := s.store.DeleteTuple(r.Context(), tupleURI); err != nil {
		writeError(r.Context(), w, err, "delete tuple")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) ListTuples(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}
	var params habitat.NetworkHabitatRelationshipListTuplesParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode params", http.StatusBadRequest)
		return
	}
	space, err := habitat_syntax.ParseSpaceURI(params.Space)
	if err != nil {
		http.Error(w, "invalid space uri", http.StatusBadRequest)
		return
	}
	if !s.requireReader(w, r, caller, space) {
		return
	}
	tuples, err := s.store.ListTuples(r.Context(), space, TupleFilter{
		ObjectURI:  params.Object,
		SubjectDID: params.SubjectDid,
		Relation:   params.Relation,
	})
	if err != nil {
		writeError(r.Context(), w, err, "list tuples")
		return
	}
	views := make([]habitat.NetworkHabitatRelationshipListTuplesTupleView, len(tuples))
	for i, t := range tuples {
		views[i] = habitat.NetworkHabitatRelationshipListTuplesTupleView{
			Uri:      t.URI.String(),
			Subject:  subjectValue(t.Subject),
			Relation: string(t.Relation),
			Object:   objectValue(t.Object),
		}
	}
	encodeJSON(r.Context(), w, habitat.NetworkHabitatRelationshipListTuplesOutput{Tuples: views})
}

func (s *Server) Check(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}
	var params habitat.NetworkHabitatRelationshipCheckParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode params", http.StatusBadRequest)
		return
	}
	space, err := habitat_syntax.ParseSpaceURI(params.Space)
	if err != nil {
		http.Error(w, "invalid space uri", http.StatusBadRequest)
		return
	}
	did, err := syntax.ParseDID(params.Did)
	if err != nil {
		http.Error(w, "invalid did", http.StatusBadRequest)
		return
	}
	if !s.requireReader(w, r, caller, space) {
		return
	}
	allowed, err := s.store.Check(r.Context(), did, Role(params.Relation), space)
	if err != nil {
		writeError(r.Context(), w, err, "check")
		return
	}
	encodeJSON(r.Context(), w, habitat.NetworkHabitatRelationshipCheckOutput{Allowed: allowed})
}

func (s *Server) ListSubjects(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}
	var params habitat.NetworkHabitatRelationshipListSubjectsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode params", http.StatusBadRequest)
		return
	}
	space, err := habitat_syntax.ParseSpaceURI(params.Space)
	if err != nil {
		http.Error(w, "invalid space uri", http.StatusBadRequest)
		return
	}
	if !s.requireReader(w, r, caller, space) {
		return
	}
	dids, err := s.store.ListSubjects(r.Context(), space, Role(params.Relation))
	if err != nil {
		writeError(r.Context(), w, err, "list subjects")
		return
	}
	out := make([]string, len(dids))
	for i, d := range dids {
		out[i] = d.String()
	}
	encodeJSON(r.Context(), w, habitat.NetworkHabitatRelationshipListSubjectsOutput{Dids: out})
}

func (s *Server) ListObjects(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}
	var params habitat.NetworkHabitatRelationshipListObjectsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode params", http.StatusBadRequest)
		return
	}
	did, err := syntax.ParseDID(params.Did)
	if err != nil {
		http.Error(w, "invalid did", http.StatusBadRequest)
		return
	}
	// Callers may only enumerate their own role assignments.
	if did != caller {
		http.Error(w, "may only list objects for the authenticated DID", http.StatusForbidden)
		return
	}
	spaceURIs, err := s.store.ListObjects(r.Context(), did, Role(params.Relation))
	if err != nil {
		writeError(r.Context(), w, err, "list objects")
		return
	}
	out := make([]string, len(spaceURIs))
	for i, u := range spaceURIs {
		out[i] = u.String()
	}
	encodeJSON(r.Context(), w, habitat.NetworkHabitatRelationshipListObjectsOutput{Spaces: out})
}

func (s *Server) CreateGroup(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatRelationshipCreateGroupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode body", http.StatusBadRequest)
		return
	}
	space, err := habitat_syntax.ParseSpaceURI(input.Space)
	if err != nil {
		http.Error(w, "invalid space uri", http.StatusBadRequest)
		return
	}
	if !s.requireManager(w, r, caller, space) {
		return
	}
	uri, err := s.store.CreateGroup(r.Context(), space, input.Name, input.Description)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		http.Error(w, "space not found", http.StatusNotFound)
		return
	} else if err != nil {
		writeError(r.Context(), w, err, "create group")
		return
	}
	encodeJSON(
		r.Context(),
		w,
		habitat.NetworkHabitatRelationshipCreateGroupOutput{Uri: uri.String()},
	)
}

func (s *Server) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatRelationshipDeleteGroupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode body", http.StatusBadRequest)
		return
	}
	_, space, _, _, _, err := habitat_syntax.ParseSpaceRecordURI(input.Uri)
	if err != nil {
		http.Error(w, "invalid group uri", http.StatusBadRequest)
		return
	}
	if !s.requireManager(w, r, caller, space) {
		return
	}
	groupURI := habitat_syntax.SpaceRecordURI(input.Uri)
	if err := s.store.DeleteGroup(r.Context(), groupURI); err != nil {
		writeError(r.Context(), w, err, "delete group")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) ListGroups(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}
	var params habitat.NetworkHabitatRelationshipListGroupsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode params", http.StatusBadRequest)
		return
	}
	space, err := habitat_syntax.ParseSpaceURI(params.Space)
	if err != nil {
		http.Error(w, "invalid space uri", http.StatusBadRequest)
		return
	}
	if !s.requireReader(w, r, caller, space) {
		return
	}
	groups, err := s.store.ListGroups(r.Context(), space)
	if err != nil {
		writeError(r.Context(), w, err, "list groups")
		return
	}
	views := make([]habitat.NetworkHabitatRelationshipListGroupsGroupView, len(groups))
	for i, g := range groups {
		views[i] = habitat.NetworkHabitatRelationshipListGroupsGroupView{
			Uri:         g.URI.String(),
			Name:        g.Name,
			Description: g.Description,
			CreatedAt:   g.CreatedAt,
		}
	}
	encodeJSON(r.Context(), w, habitat.NetworkHabitatRelationshipListGroupsOutput{Groups: views})
}
