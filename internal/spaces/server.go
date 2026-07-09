package spaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	spaceType, ok := httpx.ParseNSIDInput(r.Context(), w, input.Type, "space type")
	if !ok {
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
		ownerDid, ok := httpx.ParseDIDInput(r.Context(), w, params.Did, "did")
		if !ok {
			return
		}
		filterOwner = &ownerDid
	}

	var filterType *syntax.NSID
	if params.Type != "" {
		t, ok := httpx.ParseNSIDInput(r.Context(), w, params.Type, "type filter")
		if !ok {
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

	spaceURI, ok := httpx.ParseSpaceURIInput(r.Context(), w, input.Space, "space uri")
	if !ok {
		return
	}

	memberDID, ok := httpx.ParseDIDInput(r.Context(), w, input.Did, "did")
	if !ok {
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
		// TODO: we don't know if they're not authorize because they're not a member or
		// because they don't have the right role. assume worst case and return not found
		// need to return a reason from authorize
		httpx.WriteSpaceNotFound(r.Context(), w, fmt.Errorf("not authorized to manage members"))
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

	spaceURI, ok := httpx.ParseSpaceURIInput(r.Context(), w, input.Space, "space uri")
	if !ok {
		return
	}

	memberDID, ok := httpx.ParseDIDInput(r.Context(), w, input.Did, "did")
	if !ok {
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
		// TODO: we don't know if they're not authorize because they're not a member or
		// because they don't have the right role. assume worst case and return not found
		// need to return a reason from authorize
		httpx.WriteSpaceNotFound(r.Context(), w, fmt.Errorf("not authorized to manage members"))
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

	spaceURI, ok := httpx.ParseSpaceURIInput(r.Context(), w, params.Space, "space uri")
	if !ok {
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
		httpx.WriteSpaceNotFound(r.Context(), w, fmt.Errorf("not a member"))
		return
	}

	repos, err := s.store.ListRepos(r.Context(), spaceURI)
	if errors.Is(err, ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(r.Context(), w, err)
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
	ctx := r.Context()
	credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.oauth, s.serviceAuth)).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatSpacePutRecordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode request body", http.StatusBadRequest)
		return
	}

	if input.Validate {
		httpx.WriteNotSupported(ctx, w, "validate is not yet supported")
	}

	spaceURI, ok := httpx.ParseSpaceURIInput(r.Context(), w, input.Space, "space uri")
	if !ok {
		return
	}

	repo, ok := httpx.ParseDIDInput(ctx, w, input.Repo, "repo")
	if !ok {
		return
	}

	if credInfo.Subject != repo {
		httpx.WriteInvalidRequest(ctx, w, "can't write to other repo", fmt.Errorf("wrong repo"))
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
		// TODO: we don't know if they're not authorize because they're not a member or
		// because they don't have the right role. assume worst case and return not found
		// need to return a reason from authorize
		httpx.WriteSpaceNotFound(r.Context(), w, fmt.Errorf("not authorized to manage members"))
		return
	}

	collection, ok := httpx.ParseNSIDInput(r.Context(), w, input.Collection, "collection")
	if !ok {
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
		repo,
		collection,
		rkey,
		value,
	)
	if errors.Is(err, ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(r.Context(), w, err)
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

	spaceURI, ok := httpx.ParseSpaceURIInput(r.Context(), w, params.Space, "space uri")
	if !ok {
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
			httpx.WriteSpaceNotFound(r.Context(), w, fmt.Errorf("not a member"))
			return
		}
	}
	collection, ok := httpx.ParseNSIDInput(r.Context(), w, params.Collection, "collection")
	if !ok {
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
	ctx := r.Context()
	credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.oauth, s.serviceAuth)).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatSpaceListRecordsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}

	spaceURI, ok := httpx.ParseSpaceURIInput(r.Context(), w, params.Space, "space uri")
	if !ok {
		return
	}

	isMember, err := s.store.IsMember(ctx, credInfo.Org.DID(), spaceURI, credInfo.Subject)
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
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	}

	var filterCollection *syntax.NSID
	if params.Collection != "" {
		c, ok := httpx.ParseNSIDInput(r.Context(), w, params.Collection, "collection filter")
		if !ok {
			return
		}
		filterCollection = &c
	}

	repo, ok := httpx.ParseDIDInput(r.Context(), w, params.Repo, "repo")
	if !ok {
		return
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
		}
		if !params.ExcludeValues {
			recViews[i].Value = rec.Value
		}
	}
	httpx.WriteJSON(r.Context(), w, habitat.NetworkHabitatSpaceListRecordsOutput{Records: recViews})
}

func (s *Server) ListRepoOps(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatSpaceListRepoOpsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}

	spaceURI, ok := httpx.ParseSpaceURIInput(r.Context(), w, params.Space, "space uri")
	if !ok {
		return
	}

	repoDID, ok := httpx.ParseDIDInput(r.Context(), w, params.Repo, "repo")
	if !ok {
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

	records, err := s.store.ListRepoOps(r.Context(), spaceURI, repoDID, params.Since, limit)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "list repo ops", http.StatusInternalServerError)
		return
	}

	ops := make([]habitat.NetworkHabitatSpaceListRepoOpsOpEntry, len(records))
	for i, rec := range records {
		ops[i] = habitat.NetworkHabitatSpaceListRepoOpsOpEntry{
			Rev:        rec.Rev,
			Collection: rec.Collection.String(),
			Rkey:       rec.Rkey.String(),
			Cid:        rec.Cid.String(),
		}
		if !params.ExcludeValues {
			ops[i].Value = rec.Value
		}
	}

	output := habitat.NetworkHabitatSpaceListRepoOpsOutput{
		Ops: ops,
	}
	if len(records) > 0 {
		output.Cursor = records[len(records)-1].Rev
	}

	httpx.WriteJSON(r.Context(), w, output)
}

func (s *Server) DeleteRecord(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.oauth, s.serviceAuth)).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatSpaceDeleteRecordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(ctx, w, err, "decode request body", http.StatusBadRequest)
		return
	}

	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}

	repo, ok := httpx.ParseDIDInput(ctx, w, input.Repo, "repo")
	if !ok {
		return
	}

	if credInfo.Subject != repo {
		httpx.WriteInvalidRequest(
			ctx,
			w,
			"can't write to other repo",
			fmt.Errorf("wrong repo"),
		)
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
		// TODO: we don't know if they're not authorize because they're not a member or
		// because they don't have the right role. assume worst case and return not found
		// need to return a reason from authorize
		httpx.WriteSpaceNotFound(ctx, w, fmt.Errorf("not authorized to delete"))
		return
	}

	collection, ok := httpx.ParseNSIDInput(ctx, w, input.Collection, "collection")
	if !ok {
		return
	}
	if collection.String() == habitat_syntax.ReservedRelationshipTupleNSID {
		httpx.WriteInvalidRequest(ctx, w, "invalid collection", err)
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

	spaceURI, ok := httpx.ParseSpaceURIInput(r.Context(), w, input.Space, "space uri")
	if !ok {
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
		// TODO: we don't know if they're not authorize because they're not a member or
		// because they don't have the right role. assume worst case and return not found
		// need to return a reason from authorize
		httpx.WriteSpaceNotFound(r.Context(), w, fmt.Errorf("not authorized to delete space"))
		return
	}

	err = s.store.DeleteSpace(r.Context(), spaceURI)
	if errors.Is(err, ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(r.Context(), w, err)
		return
	} else if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "delete space", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
}
