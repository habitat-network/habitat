package spaces

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"github.com/gorilla/schema"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/fgastore"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

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

// authorize checks if the caller has the given relation on the space via FGA,
// using the owner contextual tuple so space owners always pass.
func (s *Server) authorize(
	ctx context.Context,
	callerDID syntax.DID,
	spaceURI habitat_syntax.SpaceURI,
	relation string,
) (bool, error) {
	return s.fga.Check(
		ctx,
		fgastore.MemberUserString(callerDID),
		relation,
		fgastore.SpaceObjectKey(spaceURI),
		ownerContextualTuple(spaceURI),
	)
}

func (s *Server) CreateSpace(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.oauth)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatSpaceCreateSpaceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(w, err, "decode request body", http.StatusBadRequest)
		return
	}

	spaceType, err := syntax.ParseNSID(input.Type)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parse space type", http.StatusBadRequest)
		return
	}

	var skey habitat_syntax.SpaceKey
	if input.Skey != "" {
		parsedKey, err := habitat_syntax.ParseSkey(input.Skey)
		if err != nil {
			utils.LogAndHTTPError(w, err, "parse space key", http.StatusBadRequest)
			return
		}
		skey = parsedKey
	}

	uri, err := s.store.CreateSpace(r.Context(), callerDID, spaceType, skey)
	if errors.Is(err, ErrSpaceAlreadyExists) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	} else if err != nil {
		utils.LogAndHTTPError(w, err, "create space", http.StatusInternalServerError)
		return
	}

	output := habitat.NetworkHabitatSpaceCreateSpaceOutput{
		Uri: uri.String(),
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(output); err != nil {
		utils.LogAndHTTPError(w, err, "encode response", http.StatusInternalServerError)
	}
}

func (s *Server) ListSpaces(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.oauth)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatSpaceListSpacesParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(w, err, "decode query params", http.StatusBadRequest)
		return
	}
	var filterOwner *syntax.DID
	if params.Did != "" {
		ownerDid, err := syntax.ParseDID(params.Did)
		if err != nil {
			utils.LogAndHTTPError(w, err, "parse did", http.StatusBadRequest)
			return
		}
		filterOwner = &ownerDid
	}

	var filterType *syntax.NSID
	if params.Type != "" {
		t, err := syntax.ParseNSID(params.Type)
		if err != nil {
			utils.LogAndHTTPError(w, err, "parse type filter", http.StatusBadRequest)
			return
		}
		filterType = &t
	}

	spaces, err := s.store.ListSpaces(r.Context(), callerDID, filterOwner, filterType)
	if err != nil {
		utils.LogAndHTTPError(w, err, "list spaces", http.StatusInternalServerError)
		return
	}

	views := make([]habitat.NetworkHabitatSpaceListSpacesSpaceView, len(spaces))
	for i, sp := range spaces {
		views[i] = habitat.NetworkHabitatSpaceListSpacesSpaceView{
			Uri:         sp.URI.String(),
			Type:        sp.Type.String(),
			Skey:        sp.Skey.String(),
			MemberCount: int64(sp.MemberCount),
		}
	}

	output := habitat.NetworkHabitatSpaceListSpacesOutput{
		Spaces: views,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(output); err != nil {
		utils.LogAndHTTPError(w, err, "encode response", http.StatusInternalServerError)
	}
}

func (s *Server) AddMember(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatSpaceAddMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(w, err, "decode request body", http.StatusBadRequest)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(input.Space)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	memberDID, err := syntax.ParseDID(input.Did)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parse member did", http.StatusBadRequest)
		return
	}

	authorized, err := s.authorize(r.Context(), callerDID, spaceURI, "can_manage_members")
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"check manage members permission",
			http.StatusInternalServerError,
		)
		return
	}
	if !authorized {
		http.Error(w, "not authorized to manage members", http.StatusForbidden)
		return
	}

	err = s.store.AddMember(r.Context(), spaceURI, memberDID)
	if errors.Is(err, ErrSpaceNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if errors.Is(err, ErrUserAlreadyMember) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	} else if err != nil {
		utils.LogAndHTTPError(w, err, "add member", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) RemoveMember(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatSpaceRemoveMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(w, err, "decode request body", http.StatusBadRequest)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(input.Space)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	memberDID, err := syntax.ParseDID(input.Did)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parse member did", http.StatusBadRequest)
		return
	}

	authorized, err := s.authorize(r.Context(), callerDID, spaceURI, "can_manage_members")
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"check manage members permission",
			http.StatusInternalServerError,
		)
		return
	}
	if !authorized {
		http.Error(w, "not authorized to manage members", http.StatusForbidden)
		return
	}

	err = s.store.RemoveMember(r.Context(), spaceURI, memberDID)
	if errors.Is(err, ErrSpaceNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if errors.Is(err, ErrNotAMember) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if errors.Is(err, ErrCannotRemoveOwner) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if err != nil {
		utils.LogAndHTTPError(w, err, "remove member", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) GetMembers(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatSpaceGetMembersParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(w, err, "decode query params", http.StatusBadRequest)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(params.Space)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	isMember, err := s.store.IsMember(r.Context(), spaceURI, callerDID)
	if err != nil {
		utils.LogAndHTTPError(w, err, "check membership", http.StatusInternalServerError)
		return
	}
	if !isMember {
		http.Error(w, "not a member of this space", http.StatusForbidden)
		return
	}

	members, err := s.store.GetMembers(r.Context(), spaceURI)
	if errors.Is(err, ErrSpaceNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		utils.LogAndHTTPError(w, err, "get members", http.StatusInternalServerError)
		return
	}

	memberViews := make([]habitat.NetworkHabitatSpaceGetMembersMember, len(members))
	for i, m := range members {
		memberViews[i] = habitat.NetworkHabitatSpaceGetMembersMember{
			Did:     m.Did.String(),
			AddedAt: m.AddedAt.Format("2006-01-02T15:04:05.000Z"),
		}
	}

	output := habitat.NetworkHabitatSpaceGetMembersOutput{
		Members: memberViews,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(output); err != nil {
		utils.LogAndHTTPError(w, err, "encode response", http.StatusInternalServerError)
	}
}

func (s *Server) PutRecord(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatSpacePutRecordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(w, err, "decode request body", http.StatusBadRequest)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(input.Space)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	authorized, err := s.authorize(r.Context(), callerDID, spaceURI, "can_write")
	if err != nil {
		utils.LogAndHTTPError(w, err, "check write permission", http.StatusInternalServerError)
		return
	}
	if !authorized {
		http.Error(w, "not authorized to write records in this space", http.StatusForbidden)
		return
	}

	collection, err := syntax.ParseNSID(input.Collection)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parse collection", http.StatusBadRequest)
		return
	}

	var rkey syntax.RecordKey
	if input.Rkey != "" {
		parsedRkey, err := syntax.ParseRecordKey(input.Rkey)
		if err != nil {
			utils.LogAndHTTPError(w, err, "parse rkey", http.StatusBadRequest)
			return
		}
		rkey = parsedRkey
	} else {
		rkey = syntax.RecordKey(uuid.New().String())
	}

	value, ok := input.Record.(map[string]any)
	if !ok {
		http.Error(w, "record must be a JSON object", http.StatusBadRequest)
		return
	}

	err = s.store.PutRecord(r.Context(), spaceURI, callerDID, collection, rkey, value)
	if errors.Is(err, ErrSpaceNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		utils.LogAndHTTPError(w, err, "put record", http.StatusInternalServerError)
		return
	}

	recordURI := spaceURI.String() + "/" + collection.String() + "/" + input.Rkey

	output := habitat.NetworkHabitatSpacePutRecordOutput{
		Uri: recordURI,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(output); err != nil {
		utils.LogAndHTTPError(w, err, "encode response", http.StatusInternalServerError)
	}
}

func (s *Server) GetRecord(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatSpaceGetRecordParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(w, err, "decode query params", http.StatusBadRequest)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(params.Space)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	isMember, err := s.store.IsMember(r.Context(), spaceURI, callerDID)
	if err != nil {
		utils.LogAndHTTPError(w, err, "check membership", http.StatusInternalServerError)
		return
	}
	if !isMember {
		http.Error(w, "not a member of this space", http.StatusForbidden)
		return
	}

	collection, err := syntax.ParseNSID(params.Collection)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parse collection", http.StatusBadRequest)
		return
	}

	rkey, err := syntax.ParseRecordKey(params.Rkey)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parse rkey", http.StatusBadRequest)
		return
	}

	owner := callerDID
	if params.Repo != "" {
		owner, err = syntax.ParseDID(params.Repo)
		if err != nil {
			utils.LogAndHTTPError(w, err, "parse repo did", http.StatusBadRequest)
			return
		}
	}

	rec, err := s.store.GetRecord(r.Context(), spaceURI, owner, collection, rkey)
	if errors.Is(err, ErrRecordNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		utils.LogAndHTTPError(w, err, "get record", http.StatusInternalServerError)
		return
	}

	uri := spaceURI.String() + "/" + collection.String() + "/" + rec.Rkey.String()
	output := habitat.NetworkHabitatSpaceGetRecordOutput{
		Uri:   uri,
		Cid:   "",
		Value: rec.Value,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(output); err != nil {
		utils.LogAndHTTPError(w, err, "encode response", http.StatusInternalServerError)
	}
}

func (s *Server) ListRecords(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatSpaceListRecordsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(w, err, "decode query params", http.StatusBadRequest)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(params.Space)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	isMember, err := s.store.IsMember(r.Context(), spaceURI, callerDID)
	if err != nil {
		utils.LogAndHTTPError(w, err, "check membership", http.StatusInternalServerError)
		return
	}
	if !isMember {
		http.Error(w, "not a member of this space", http.StatusForbidden)
		return
	}

	var filterCollection *syntax.NSID
	if params.Collection != "" {
		c, err := syntax.ParseNSID(params.Collection)
		if err != nil {
			utils.LogAndHTTPError(w, err, "parse collection filter", http.StatusBadRequest)
			return
		}
		filterCollection = &c
	}

	var repo syntax.DID
	if params.Repo != "" {
		r, err := syntax.ParseDID(params.Repo)
		if err != nil {
			utils.LogAndHTTPError(w, err, "parse repo did", http.StatusBadRequest)
			return
		}
		repo = r
	} else {
		repo = callerDID
	}

	records, err := s.store.ListRecords(r.Context(), spaceURI, repo, filterCollection)
	if err != nil {
		utils.LogAndHTTPError(w, err, "list records", http.StatusInternalServerError)
		return
	}

	recViews := make([]habitat.NetworkHabitatSpaceListRecordsRecord, len(records))
	for i, rec := range records {
		recViews[i] = habitat.NetworkHabitatSpaceListRecordsRecord{
			Collection: rec.Collection.String(),
			Rkey:       rec.Rkey.String(),
			Cid:        "",
			UpdatedAt:  rec.UpdatedAt.Format("2006-01-02T15:04:05.000Z"),
			Value:      rec.Value,
			Did:        rec.Owner.String(),
		}
	}

	output := habitat.NetworkHabitatSpaceListRecordsOutput{
		Records: recViews,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(output); err != nil {
		utils.LogAndHTTPError(w, err, "encode response", http.StatusInternalServerError)
	}
}

func (s *Server) DeleteRecord(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatSpaceDeleteRecordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(w, err, "decode request body", http.StatusBadRequest)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(input.Space)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	authorized, err := s.authorize(r.Context(), callerDID, spaceURI, "can_delete")
	if err != nil {
		utils.LogAndHTTPError(w, err, "check delete permission", http.StatusInternalServerError)
		return
	}
	if !authorized {
		http.Error(w, "not authorized to delete records in this space", http.StatusForbidden)
		return
	}

	collection, err := syntax.ParseNSID(input.Collection)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parse collection", http.StatusBadRequest)
		return
	}

	err = s.store.DeleteRecord(r.Context(), spaceURI, collection, input.Rkey)
	if err != nil {
		utils.LogAndHTTPError(w, err, "delete record", http.StatusInternalServerError)
		return
	}

	output := habitat.NetworkHabitatSpaceDeleteRecordOutput{}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(output); err != nil {
		utils.LogAndHTTPError(w, err, "encode response", http.StatusInternalServerError)
	}
}
