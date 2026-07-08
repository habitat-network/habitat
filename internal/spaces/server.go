package spaces

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
	"github.com/habitat-network/habitat/internal/org"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

type Server struct {
	store       Store
	fga         fgastore.Store
	oauth       authn.Method
	serviceAuth authn.Method
	decoder     *schema.Decoder
	orgStore    org.Store
}

func NewServer(
	store Store,
	fga fgastore.Store,
	oauth authn.Method,
	serviceAuth authn.Method,
	orgStore org.Store,
) *Server {
	return &Server{
		store:       store,
		fga:         fga,
		oauth:       oauth,
		serviceAuth: serviceAuth,
		decoder:     schema.NewDecoder(),
		orgStore:    orgStore,
	}
}

// authorize checks if the caller has the given relation on the space via FGA,
// using the owner contextual tuple so space owners always pass.
func (s *Server) authorize(
	ctx context.Context,
	callerOrg syntax.DID,
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
		fgastore.OrgMemberContextualTuple(callerOrg),
	)
}

func (s *Server) CreateSpace(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth),
		authn.WithSupportedCredentials(authn.UserCredential, authn.OrgCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatSpaceCreateSpaceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode request body", http.StatusBadRequest)
		return
	}

	spaceType, err := syntax.ParseNSID(input.Type)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse space type", http.StatusBadRequest)
		return
	}

	var skey habitat_syntax.SpaceKey
	if input.Skey != "" {
		parsedKey, err := habitat_syntax.ParseSkey(input.Skey)
		if err != nil {
			utils.LogAndHTTPError(r.Context(), w, err, "parse space key", http.StatusBadRequest)
			return
		}
		skey = parsedKey
	}

	callerOrg, _, err := s.orgStore.GetOrgForDID(r.Context(), credInfo.Subject)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"get org for caller",
			http.StatusInternalServerError,
		)
		return
	}

	uri, err := s.store.CreateSpace(r.Context(), callerOrg.DID(), credInfo.Subject, spaceType, skey)
	if errors.Is(err, ErrSpaceAlreadyExists) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	} else if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "create space", http.StatusInternalServerError)
		return
	}

	output := habitat.NetworkHabitatSpaceCreateSpaceOutput{
		Uri: uri.String(),
	}
	httpx.WriteJSON(r.Context(), w, output)
}

func (s *Server) ListSpaces(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth),
		authn.WithSupportedCredentials(authn.UserCredential, authn.OrgCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatSpaceListSpacesParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}
	var filterOwner *syntax.DID
	if params.Did != "" {
		ownerDid, err := syntax.ParseDID(params.Did)
		if err != nil {
			utils.LogAndHTTPError(r.Context(), w, err, "parse did", http.StatusBadRequest)
			return
		}
		filterOwner = &ownerDid
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

	spaces, err := s.store.ListSpaces(
		r.Context(),
		credInfo.Org.DID(),
		credInfo.Subject,
		filterOwner,
		filterType,
	)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "list spaces", http.StatusInternalServerError)
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
	httpx.WriteJSON(r.Context(), w, output)
}

func (s *Server) AddMember(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
		authn.WithSupportedCredentials(authn.UserCredential, authn.OrgCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatSpaceAddMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode request body", http.StatusBadRequest)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(input.Space)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	memberDID, err := syntax.ParseDID(input.Did)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse member did", http.StatusBadRequest)
		return
	}

	authorized, err := s.authorize(
		r.Context(),
		credInfo.Org.DID(),
		credInfo.Subject,
		spaceURI,
		fgastore.RelationSpaceMemberManager,
	)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
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

	access, err := ParseSpaceAccess(input.Access)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse access", http.StatusBadRequest)
		return
	}

	err = s.store.AddMember(r.Context(), spaceURI, memberDID, access)
	if errors.Is(err, ErrSpaceNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if errors.Is(err, ErrUserAlreadyMember) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	} else if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "add member", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) RemoveMember(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
		authn.WithSupportedCredentials(authn.UserCredential, authn.OrgCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatSpaceRemoveMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode request body", http.StatusBadRequest)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(input.Space)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	memberDID, err := syntax.ParseDID(input.Did)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse member did", http.StatusBadRequest)
		return
	}

	authorized, err := s.authorize(
		r.Context(),
		credInfo.Org.DID(),
		credInfo.Subject,
		spaceURI,
		fgastore.RelationSpaceMemberManager,
	)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
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
	} else if errors.Is(err, ErrCannotRemoveOrg) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "remove member", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) ListRepos(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.oauth, s.serviceAuth)).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatSpaceListReposParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}

	if params.Cursor != "" || params.Limit != 0 {
		http.Error(w, "cursor and limit are not yet supported", http.StatusNotImplemented)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(params.Space)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	isMember, err := s.store.IsMember(r.Context(), credInfo.Org.DID(), spaceURI, credInfo.Subject)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"check membership",
			http.StatusInternalServerError,
		)
		return
	}
	if !isMember {
		http.Error(w, "not a member of this space", http.StatusForbidden)
		return
	}

	repos, err := s.store.ListRepos(r.Context(), spaceURI)
	if errors.Is(err, ErrSpaceNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "list repos", http.StatusInternalServerError)
		return
	}

	repoViews := make([]habitat.NetworkHabitatSpaceListReposRepo, len(repos))
	for i, r := range repos {
		repoViews[i] = habitat.NetworkHabitatSpaceListReposRepo{
			Did:  r.DID.String(),
			Rev:  r.Rev,
			Hash: nil,
		}
	}

	output := habitat.NetworkHabitatSpaceListReposOutput{
		Repos: repoViews,
	}
	httpx.WriteJSON(r.Context(), w, output)
}

func (s *Server) PutRecord(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
		authn.WithRequiredSubject(),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatSpacePutRecordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode request body", http.StatusBadRequest)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(input.Space)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	authorized, err := s.authorize(
		r.Context(),
		credInfo.Org.DID(),
		credInfo.Subject,
		spaceURI,
		fgastore.RelationSpaceWriter,
	)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"check write permission",
			http.StatusInternalServerError,
		)
		return
	}
	if !authorized {
		http.Error(w, "not authorized to write records in this space", http.StatusForbidden)
		return
	}

	collection, err := syntax.ParseNSID(input.Collection)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse collection", http.StatusBadRequest)
		return
	}
	if collection.String() == habitat_syntax.ReservedRelationshipTupleNSID {
		http.Error(
			w,
			"relationship tuples must be managed via network.habitat.relationship.* endpoints",
			http.StatusForbidden,
		)
		return
	}

	var rkey syntax.RecordKey
	if input.Rkey != "" {
		parsedRkey, err := syntax.ParseRecordKey(input.Rkey)
		if err != nil {
			utils.LogAndHTTPError(r.Context(), w, err, "parse rkey", http.StatusBadRequest)
			return
		}
		rkey = parsedRkey
	}

	value, ok := input.Record.(map[string]any)
	if !ok {
		http.Error(w, "record must be a JSON object", http.StatusBadRequest)
		return
	}

	recordUri, cid, err := s.store.PutRecord(
		r.Context(),
		spaceURI,
		credInfo.Subject,
		collection,
		rkey,
		value,
	)
	if errors.Is(err, ErrSpaceNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "put record", http.StatusInternalServerError)
		return
	}

	output := habitat.NetworkHabitatSpacePutRecordOutput{
		Uri: recordUri.String(),
		Cid: cid.String(),
	}
	httpx.WriteJSON(r.Context(), w, output)
}

func (s *Server) GetRecord(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatSpaceGetRecordParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(params.Space)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse space uri", http.StatusBadRequest)
		return
	}
	if credInfo.Subject != "" {
		isMember, err := s.store.IsMember(
			r.Context(),
			credInfo.Org.DID(),
			spaceURI,
			credInfo.Subject,
		)
		if err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"check membership",
				http.StatusInternalServerError,
			)
			return
		}
		if !isMember {
			http.Error(w, "not a member of this space", http.StatusForbidden)
			return
		}
	}
	collection, err := syntax.ParseNSID(params.Collection)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse collection", http.StatusBadRequest)
		return
	}

	rkey, err := syntax.ParseRecordKey(params.Rkey)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse rkey", http.StatusBadRequest)
		return
	}

	owner := credInfo.Subject
	if params.Repo != "" {
		owner, err = syntax.ParseDID(params.Repo)
		if err != nil {
			utils.LogAndHTTPError(r.Context(), w, err, "parse repo did", http.StatusBadRequest)
			return
		}
	}

	rec, err := s.store.GetRecord(r.Context(), spaceURI, owner, collection, rkey)
	if errors.Is(err, ErrRecordNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "get record", http.StatusInternalServerError)
		return
	}

	uri := habitat_syntax.ConstructSpaceRecordURI(spaceURI, owner, collection, rec.Rkey)
	output := habitat.NetworkHabitatSpaceGetRecordOutput{
		Uri:   uri.String(),
		Cid:   rec.Cid.String(),
		Value: rec.Value,
	}
	httpx.WriteJSON(r.Context(), w, output)
}

func (s *Server) ListRecords(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatSpaceListRecordsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(params.Space)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	if credInfo.Subject != "" {
		isMember, err := s.store.IsMember(
			r.Context(),
			credInfo.Org.DID(),
			spaceURI,
			credInfo.Subject,
		)
		if err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"check membership",
				http.StatusInternalServerError,
			)
			return
		}
		if !isMember {
			http.Error(w, "not a member of this space", http.StatusForbidden)
			return
		}
	}

	var filterCollection *syntax.NSID
	if params.Collection != "" {
		c, err := syntax.ParseNSID(params.Collection)
		if err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"parse collection filter",
				http.StatusBadRequest,
			)
			return
		}
		filterCollection = &c
	}

	var repo syntax.DID
	if params.Repo != "" {
		parsedRepo, err := syntax.ParseDID(params.Repo)
		if err != nil {
			utils.LogAndHTTPError(r.Context(), w, err, "parse repo did", http.StatusBadRequest)
			return
		}
		repo = parsedRepo
	} else {
		repo = credInfo.Subject
	}

	records, err := s.store.ListRecords(r.Context(), spaceURI, repo, filterCollection)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "list records", http.StatusInternalServerError)
		return
	}

	recViews := make([]habitat.NetworkHabitatSpaceListRecordsRecord, len(records))
	for i, rec := range records {
		recViews[i] = habitat.NetworkHabitatSpaceListRecordsRecord{
			Collection: rec.Collection.String(),
			Rkey:       rec.Rkey.String(),
			Cid:        rec.Cid.String(),
			UpdatedAt:  rec.UpdatedAt.Format("2006-01-02T15:04:05.000Z"),
			Value:      rec.Value,
		}
	}

	output := habitat.NetworkHabitatSpaceListRecordsOutput{
		Records: recViews,
	}
	httpx.WriteJSON(r.Context(), w, output)
}

func (s *Server) GetRepoOplog(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatSpaceGetRepoOplogParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(params.Space)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	repoDID, err := syntax.ParseDID(params.Repo)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse repo did", http.StatusBadRequest)
		return
	}

	if credInfo.Subject != "" {
		isMember, err := s.store.IsMember(
			r.Context(),
			credInfo.Org.DID(),
			spaceURI,
			credInfo.Subject,
		)
		if err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"check membership",
				http.StatusInternalServerError,
			)
			return
		}
		if !isMember {
			http.Error(w, "not a member of this space", http.StatusForbidden)
			return
		}
	}

	limit := int(params.Limit)
	if limit <= 0 {
		limit = 100
	}

	records, err := s.store.GetRepoOplog(r.Context(), spaceURI, repoDID, params.Since, limit)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "get repo oplog", http.StatusInternalServerError)
		return
	}

	recViews := make([]habitat.NetworkHabitatSpaceGetRepoOplogRecord, len(records))
	for i, rec := range records {
		recViews[i] = habitat.NetworkHabitatSpaceGetRepoOplogRecord{
			Rev:        rec.Rev,
			Collection: rec.Collection.String(),
			Rkey:       rec.Rkey.String(),
			Value:      rec.Value,
			Cid:        rec.Cid.String(),
		}
	}

	output := habitat.NetworkHabitatSpaceGetRepoOplogOutput{
		Records: recViews,
	}
	if len(records) > 0 {
		output.Cursor = records[len(records)-1].Rev
	}

	httpx.WriteJSON(r.Context(), w, output)
}

func (s *Server) DeleteRecord(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
		authn.WithRequiredSubject(),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatSpaceDeleteRecordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode request body", http.StatusBadRequest)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(input.Space)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	authorized, err := s.authorize(
		r.Context(),
		credInfo.Org.DID(),
		credInfo.Subject,
		spaceURI,
		fgastore.RelationSpaceOwner,
	)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"check delete permission",
			http.StatusInternalServerError,
		)
		return
	}
	if !authorized {
		http.Error(w, "not authorized to delete records in this space", http.StatusForbidden)
		return
	}

	collection, err := syntax.ParseNSID(input.Collection)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse collection", http.StatusBadRequest)
		return
	}
	if collection.String() == habitat_syntax.ReservedRelationshipTupleNSID {
		http.Error(
			w,
			"relationship tuples must be managed via network.habitat.relationship.* endpoints",
			http.StatusForbidden,
		)
		return
	}

	err = s.store.DeleteRecord(r.Context(), spaceURI, collection, input.Rkey)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "delete record", http.StatusInternalServerError)
		return
	}

	output := habitat.NetworkHabitatSpaceDeleteRecordOutput{}
	httpx.WriteJSON(r.Context(), w, output)
}

func (s *Server) DeleteSpace(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
		authn.WithRequiredSubject(),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatSpaceDeleteSpaceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode request body", http.StatusBadRequest)
		return
	}

	spaceURI, err := habitat_syntax.ParseSpaceURI(input.Space)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parse space uri", http.StatusBadRequest)
		return
	}

	authorized, err := s.authorize(
		r.Context(),
		credInfo.Org.DID(),
		credInfo.Subject,
		spaceURI,
		fgastore.RelationSpaceOwner,
	)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"check owner permission",
			http.StatusInternalServerError,
		)
		return
	}
	if !authorized {
		http.Error(w, "not authorized to delete this space", http.StatusForbidden)
		return
	}

	err = s.store.DeleteSpace(r.Context(), spaceURI)
	if errors.Is(err, ErrSpaceNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "delete space", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
}
