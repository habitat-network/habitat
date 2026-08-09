package spaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/schema"
	"github.com/ipfs/go-cid"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/spacecommit"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

// errEmptyRepo signals that a repo holds no records, so there is nothing to
// commit over. It is handled internally and never returned to clients.
var errEmptyRepo = errors.New("repo has no records")

type Server struct {
	store       Store
	fga         fgastore.Store
	oauth       authn.Method
	serviceAuth authn.Method
	delegation  authn.Method
	spaceToken  authn.Method
	decoder     *schema.Decoder
	orgStore    org.Store
	commit      *spacecommit.Authority
	hive        hive.Hive
	blobs       BlobStore
	hostKey     atcrypto.PrivateKey
}

// NewServer constructs the spaces server. host and member are the commit
// signers: member signs commits for habitat-managed repo owners with their own
// key (proposal spec), and host signs for owners on external PDSes with
// Habitat's single host key. Either may be nil; when neither can sign an
// author, listRepoOps omits the signed commit. blobs backs the uploadBlob and
// getBlob endpoints.
func NewServer(
	store Store,
	fga fgastore.Store,
	oauth authn.Method,
	serviceAuth authn.Method,
	delegation *authn.DelegationTokenAuthMethod,
	spaceToken authn.Method,
	orgStore org.Store,
	hostPrivateKey atcrypto.PrivateKey,
	hive hive.Hive,
	blobs BlobStore,
) *Server {
	return &Server{
		store:       store,
		fga:         fga,
		oauth:       oauth,
		serviceAuth: serviceAuth,
		spaceToken:  spaceToken,
		decoder:     schema.NewDecoder(),
		orgStore:    orgStore,
		commit:      spacecommit.NewAuthority(hostPrivateKey, hive),
		hive:        hive,
		delegation:  delegation,
		blobs:       blobs,
		hostKey:     hostPrivateKey,
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
	ctx := r.Context()
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth),
		authn.WithSupportedCredentials(authn.UserCredential, authn.OrgCredential),
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
	callerOrg, _, err := s.orgStore.GetOrgForDID(ctx, credInfo.Subject)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get org for caller: %w", err))
		return
	}
	uri, err := s.store.CreateSpace(ctx, callerOrg.DID(), credInfo.Subject, spaceType, skey)
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

func (s *Server) ListSpaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.oauth, s.serviceAuth)).Validate(w, r)
	if !ok {
		return
	}
	var params habitat.NetworkHabitatSpaceListSpacesParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode query params", err)
		return
	}
	var filterOwner *syntax.DID
	if params.Did != "" {
		ownerDid, ok := httpx.ParseDIDInput(ctx, w, params.Did, "did")
		if !ok {
			return
		}
		filterOwner = &ownerDid
	}
	var filterType *syntax.NSID
	if params.Type != "" {
		t, ok := httpx.ParseNSIDInput(ctx, w, params.Type, "type filter")
		if !ok {
			return
		}
		filterType = &t
	}
	spaces, err := s.store.ListSpaces(
		ctx,
		credInfo.Subject,
		filterOwner,
		filterType,
	)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list spaces: %w", err))
		return
	}
	views := make([]habitat.NetworkHabitatSpaceListSpacesSpaceView, len(spaces))
	for i, uri := range spaces {
		views[i] = habitat.NetworkHabitatSpaceListSpacesSpaceView{
			Uri:     uri.String(),
			IsOwner: uri.SpaceOwner() == credInfo.Subject,
		}
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceListSpacesOutput{
		Spaces: views,
	})
}

func (s *Server) AddMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
		authn.WithSupportedCredentials(authn.UserCredential, authn.OrgCredential),
	).Validate(w, r)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatSimplespaceAddMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	memberDID, ok := httpx.ParseDIDInput(ctx, w, input.Did, "did")
	if !ok {
		return
	}
	authorized, err := s.authorize(
		ctx,
		credInfo.Org.DID(),
		credInfo.Subject,
		spaceURI,
		fgastore.RelationSpaceMemberManager,
	)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check manage members permission: %w", err))
		return
	}
	if !authorized {
		// TODO: we don't know if they're not authorize because they're not a member or
		// because they don't have the right role. assume worst case and return not found
		// need to return a reason from authorize
		httpx.WriteSpaceNotFound(ctx, w, fmt.Errorf("not authorized to manage members"))
		return
	}
	err = s.store.AddMember(r.Context(), spaceURI, memberDID, SpaceAccessWrite)
	if errors.Is(err, ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if errors.Is(err, ErrUserAlreadyMember) {
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("add member: %w", err))
		return
	}
}

func (s *Server) RemoveMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
		authn.WithSupportedCredentials(authn.UserCredential, authn.OrgCredential),
	).Validate(w, r)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatSimplespaceRemoveMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	memberDID, ok := httpx.ParseDIDInput(ctx, w, input.Did, "did")
	if !ok {
		return
	}
	authorized, err := s.authorize(
		ctx,
		credInfo.Org.DID(),
		credInfo.Subject,
		spaceURI,
		fgastore.RelationSpaceMemberManager,
	)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check manage members permission: %w", err))
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
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if errors.Is(err, ErrNotAMember) {
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
	credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.oauth, s.serviceAuth)).Validate(w, r)
	if !ok {
		return
	}
	var params habitat.NetworkHabitatSimplespaceListMembersParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	users, err := s.fga.ListUsers(
		ctx,
		fgastore.SpaceObjectKey(spaceURI),
		fgastore.RelationSpaceReader,
		ownerContextualTuple(spaceURI),
		fgastore.OrgMemberContextualTuple(credInfo.Org.DID()),
	)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list members: %w", err))
		return
	}
	var members []habitat.NetworkHabitatSimplespaceListMembersMember
	for _, user := range users {
		did, err := fgastore.MemberUserToDID(user)
		if err != nil {
			slog.ErrorContext(ctx, "failed to convert user to did", "user", user, "err", err)
			continue
		}
		members = append(
			members,
			habitat.NetworkHabitatSimplespaceListMembersMember{Did: did.String()},
		)
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSimplespaceListMembersOutput{Members: members})
}

// spaceAuthorized checks the credential may read spaceURI: a space credential
// must name exactly that space (enforced by the validator's WithSpace option),
// and any other credential must belong to a member of the space. It writes the
// error response and returns false when the caller is not authorized.
func (s *Server) spaceAuthorized(
	ctx context.Context,
	w http.ResponseWriter,
	credInfo *authn.CredentialInfo,
	spaceURI habitat_syntax.SpaceURI,
) bool {
	if credInfo.Space != "" {
		if credInfo.Space != spaceURI {
			httpx.WriteInvalidRequest(ctx, w, "credential does not authorize this space",
				fmt.Errorf("credential space %q does not match %q", credInfo.Space, spaceURI))
			return false
		}
		return true
	}

	member, err := s.store.IsMember(ctx, credInfo.Org.DID(), spaceURI, credInfo.Subject)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check membership: %w", err))
		return false
	}
	if !member {
		httpx.WriteSpaceNotFound(ctx, w, fmt.Errorf("not a member"))
		return false
	}
	return true
}

func (s *Server) ListRepos(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatSpaceListReposParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.oauth, s.serviceAuth, s.spaceToken), authn.WithSpace(spaceURI)).
		Validate(w, r)
	if !ok {
		return
	}
	if !s.spaceAuthorized(ctx, w, credInfo, spaceURI) {
		return
	}

	repos, err := s.store.ListRepos(r.Context(), spaceURI)
	if errors.Is(err, ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list repos: %w", err))
		return
	}
	repoViews := make([]habitat.NetworkHabitatSpaceListReposRepo, len(repos))
	for i, r := range repos {
		repoViews[i] = habitat.NetworkHabitatSpaceListReposRepo{
			Did:  r.DID.String(),
			Rev:  r.Rev,
			Hash: r.Hash,
		}
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceListReposOutput{
		Repos: repoViews,
	})
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
		return
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
	collection, ok := httpx.ParseNSIDInput(ctx, w, input.Collection, "collection")
	if !ok {
		return
	}
	if collection.String() == habitat_syntax.ReservedRelationshipTupleNSID {
		httpx.WriteInvalidRequest(ctx, w,
			"relationship tuples must be managed via network.habitat.relationship.* endpoints", nil)
		return
	}
	var rkey syntax.RecordKey
	if input.Rkey != "" {
		parsedRkey, err := syntax.ParseRecordKey(input.Rkey)
		if err != nil {
			httpx.WriteInvalidRequest(ctx, w, "invalid rkey", err)
			return
		}
		rkey = parsedRkey
	}
	value, ok := input.Record.(map[string]any)
	if !ok {
		httpx.WriteInvalidRequest(ctx, w, "record must be a JSON object", nil)
		return
	}
	authorized, err := s.authorize(
		ctx,
		credInfo.Org.DID(),
		credInfo.Subject,
		spaceURI,
		fgastore.RelationSpaceWriter,
	)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("authorize: %w", err))
		return
	}
	if !authorized {
		// TODO: we don't know if they're not authorize because they're not a member or
		// because they don't have the right role. assume worst case and return not found
		// need to return a reason from authorize
		httpx.WriteSpaceNotFound(ctx, w, fmt.Errorf("not authorized to manage members"))
		return
	}
	recordURI, cid, err := s.store.PutRecord(
		ctx,
		spaceURI,
		repo,
		collection,
		rkey,
		value,
	)
	if errors.Is(err, ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("put record: %w", err))
		return
	}
	httpx.WriteJSON(r.Context(), w, habitat.NetworkHabitatSpacePutRecordOutput{
		Uri: recordURI.String(),
		Cid: cid.String(),
	})
}

func (s *Server) GetRecord(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatSpaceGetRecordParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth, s.spaceToken),
		authn.WithSpace(spaceURI),
	).Validate(w, r)
	if !ok {
		return
	}
	if !s.spaceAuthorized(ctx, w, credInfo, spaceURI) {
		return
	}
	collection, ok := httpx.ParseNSIDInput(ctx, w, params.Collection, "collection")
	if !ok {
		return
	}
	rkey, err := syntax.ParseRecordKey(params.Rkey)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid rkey", err)
		return
	}
	repo, ok := httpx.ParseDIDInput(ctx, w, params.Repo, "repo")
	if !ok {
		return
	}
	rec, err := s.store.GetRecord(ctx, spaceURI, repo, collection, rkey)
	if errors.Is(err, ErrRecordNotFound) {
		httpx.WriteRecordNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get record: %w", err))
		return
	}
	httpx.WriteJSON(r.Context(), w, habitat.NetworkHabitatSpaceGetRecordOutput{
		Uri: habitat_syntax.ConstructSpaceRecordURI(spaceURI, repo, collection, rec.Rkey).
			String(),
		Cid:   rec.Cid.String(),
		Value: rec.Value,
	})
}

// UploadBlob stores an uploaded blob content-addressed by its CID and returns
// the blob reference. Implements network.habitat.repo.uploadBlob.
func (s *Server) UploadBlob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth),
	).Validate(w, r)
	if !ok {
		return
	}
	mimeType := r.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 500*1024))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		httpx.WriteError(ctx, w, "BlobTooLarge", "max 500kb", http.StatusRequestEntityTooLarge)
		return
	} else if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to read request body", err)
		return
	}
	c, size, err := s.blobs.PutBlob(ctx, mimeType, data)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("store blob: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatRepoUploadBlobOutput{
		Cid: c.String(),
		Blob: atdata.Blob{
			Ref:      atdata.CIDLink(c),
			MimeType: mimeType,
			Size:     size,
		},
	})
}

// GetBlob streams a blob stored within a space back to a caller with read
// access. Implements network.habitat.space.getBlob.
func (s *Server) GetBlob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatSpaceGetBlobParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.oauth, s.serviceAuth, s.spaceToken), authn.WithSpace(spaceURI)).
		Validate(w, r)
	if !ok {
		return
	}
	if !s.spaceAuthorized(ctx, w, credInfo, spaceURI) {
		return
	}
	c, err := cid.Parse(params.Cid)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse cid", err)
		return
	}
	mimeType, data, err := s.blobs.GetBlob(ctx, c)
	if errors.Is(err, ErrBlobNotFound) {
		httpx.WriteError(ctx, w, "BlobNotFound", "blob not found", http.StatusNotFound)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get blob: %w", err))
		return
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if _, err := w.Write(data); err != nil {
		slog.ErrorContext(ctx, "failed to write blob", "err", err)
		return
	}
}

func (s *Server) ListRecords(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatSpaceListRecordsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.oauth, s.serviceAuth, s.spaceToken), authn.WithSpace(spaceURI)).
		Validate(w, r)
	if !ok {
		return
	}
	if !s.spaceAuthorized(ctx, w, credInfo, spaceURI) {
		return
	}
	var filterCollection *syntax.NSID
	if params.Collection != "" {
		c, ok := httpx.ParseNSIDInput(ctx, w, params.Collection, "collection filter")
		if !ok {
			return
		}
		filterCollection = &c
	}
	repo, ok := httpx.ParseDIDInput(ctx, w, params.Repo, "repo")
	if !ok {
		return
	}
	records, err := s.store.ListRecords(r.Context(), spaceURI, repo, filterCollection)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list records: %w", err))
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
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceListRecordsOutput{Records: recViews})
}

func (s *Server) GetRepo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatSpaceGetRepoParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.oauth, s.serviceAuth, s.spaceToken), authn.WithSpace(spaceURI)).
		Validate(w, r)
	if !ok {
		return
	}
	repoDID, ok := httpx.ParseDIDInput(ctx, w, params.Repo, "repo")
	if !ok {
		return
	}
	if !s.spaceAuthorized(ctx, w, credInfo, spaceURI) {
		return
	}
	// Sign the repo's current head; the signed commit is the CAR's first root. An
	// empty repo has no state to recover, so it reports as not found.
	commit, err := s.signRepoHead(ctx, spaceURI, repoDID)
	switch {
	case err == nil:
	case errors.Is(err, errEmptyRepo):
		httpx.WriteRepoNotFound(ctx, w, err)
		return
	default:
		httpx.WriteServerError(ctx, w, fmt.Errorf("sign repo head: %w", err))
		return
	}

	blocks, err := s.store.ListRecordBlocks(ctx, spaceURI, repoDID)
	if errors.Is(err, ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list blocks: %w", err))
		return
	}

	carBytes, err := SerializeRepoCAR(commit, blocks)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("serialize car: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/vnd.ipld.car")
	if _, err := w.Write(carBytes); err != nil {
		utils.LogAndHTTPError(ctx, w, err, "write car", http.StatusInternalServerError)
	}
}

func (s *Server) ListRepoOps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatSpaceListRepoOpsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid query params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth, s.spaceToken),
		authn.WithSpace(spaceURI),
	).Validate(w, r)
	if !ok {
		return
	}
	repoDID, ok := httpx.ParseDIDInput(ctx, w, params.Repo, "repo")
	if !ok {
		return
	}
	if !s.spaceAuthorized(ctx, w, credInfo, spaceURI) {
		return
	}

	limit := int(params.Limit)
	if limit <= 0 {
		limit = 100
	}

	records, err := s.store.ListRepoOps(ctx, spaceURI, repoDID, params.Since, limit)
	if errors.Is(err, ErrRevTooFar) {
		httpx.WriteError(ctx, w, "RevNotFound",
			"since revision is ahead of the repo head", http.StatusBadRequest)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list repo ops: %w", err))
		return
	}

	ops := make([]habitat.NetworkHabitatSpaceListRepoOpsOpEntry, len(records))
	for i, rec := range records {
		ops[i] = habitat.NetworkHabitatSpaceListRepoOpsOpEntry{
			Rev:        rec.Rev,
			Collection: rec.Collection.String(),
			Rkey:       rec.Rkey.String(),
			Prev:       rec.Prev,
			Cid:        rec.Cid.String(),
		}
		if !params.ExcludeValues {
			ops[i].Value = rec.Value
		}
	}

	output := habitat.NetworkHabitatSpaceListRepoOpsOutput{Ops: ops}
	if len(records) > 0 {
		output.Cursor = records[len(records)-1].Rev
	}

	// When this page reaches the head of the oplog (fewer ops than the limit) and
	// a signer is available for the repo owner, include the current signed commit
	// so a syncer can authenticate the state it has folded up to this point.
	if len(records) < limit {
		commit, err := s.buildRepoCommit(ctx, spaceURI, repoDID)
		switch err {
		case nil:
			output.Commit = commit
		default:
			httpx.WriteServerError(ctx, w, fmt.Errorf("build repo commit: %w", err))
			return
		}
	}
	httpx.WriteJSON(r.Context(), w, output)
}

// signRepoHead computes and signs the repo's current head commit over its cached
// LtHash. It returns errEmptyRepo when the repo holds no records
func (s *Server) signRepoHead(
	ctx context.Context,
	spaceURI habitat_syntax.SpaceURI,
	repo syntax.DID,
) (spacecommit.SignedCommit, error) {
	rev, hash, found, err := s.store.RepoHead(ctx, spaceURI, repo)
	if err != nil {
		return spacecommit.SignedCommit{}, fmt.Errorf("repo head: %w", err)
	}
	if !found {
		return spacecommit.SignedCommit{}, errEmptyRepo
	}
	return s.commit.Build(ctx, spaceURI, repo, rev, hash)
}

// buildRepoCommit signs the repo's current head and shapes it as the lexicon
// signedCommit for JSON responses. It surfaces errEmptyRepo
// from signRepoHead so callers can omit the commit.
func (s *Server) buildRepoCommit(
	ctx context.Context,
	spaceURI habitat_syntax.SpaceURI,
	repo syntax.DID,
) (habitat.NetworkHabitatSpaceDefsSignedCommit, error) {
	commit, err := s.signRepoHead(ctx, spaceURI, repo)
	if err != nil {
		return habitat.NetworkHabitatSpaceDefsSignedCommit{}, err
	}
	return habitat.NetworkHabitatSpaceDefsSignedCommit{
		Ver:  int64(commit.Ver),
		Hash: commit.Hash,
		Ikm:  commit.Ikm,
		Mac:  commit.Mac,
		Sig:  commit.Sig,
		Rev:  commit.Rev,
	}, nil
}

// GetLatestCommit returns the current signed commit over a repo's head state
// within a space. It is callable with OAuth (for the caller's own data) or a
// space credential (for syncing services); either way the caller must be a
// member of the space.
func (s *Server) GetLatestCommit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatSpaceGetLatestCommitParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid query params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.oauth, s.serviceAuth, s.spaceToken), authn.WithSpace(spaceURI)).
		Validate(w, r)
	if !ok {
		return
	}
	repoDID, ok := httpx.ParseDIDInput(ctx, w, params.Repo, "repo")
	if !ok {
		return
	}
	if !s.spaceAuthorized(ctx, w, credInfo, spaceURI) {
		return
	}

	commit, err := s.buildRepoCommit(ctx, spaceURI, repoDID)
	switch {
	case err == nil:
	case errors.Is(err, errEmptyRepo):
		httpx.WriteRepoNotFound(ctx, w, err)
		return
	default:
		httpx.WriteServerError(ctx, w, fmt.Errorf("build repo commit: %w", err))
		return
	}

	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceGetLatestCommitOutput{Commit: commit})
}

func (s *Server) DeleteRecord(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.oauth, s.serviceAuth)).Validate(w, r)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatSpaceDeleteRecordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
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
		httpx.WriteInvalidRequest(ctx, w, "can't write to other repo", nil)
	}
	collection, ok := httpx.ParseNSIDInput(ctx, w, input.Collection, "collection")
	if !ok {
		return
	}
	if collection.String() == habitat_syntax.ReservedRelationshipTupleNSID {
		httpx.WriteInvalidRequest(ctx, w, "invalid collection", nil)
		return
	}
	if err := s.store.DeleteRecord(ctx, spaceURI, repo, collection, input.Rkey); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("delete record: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceDeleteRecordOutput{})
}

func (s *Server) DeleteSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth, s.serviceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatSimplespaceDeleteSpaceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	authorized, err := s.authorize(
		ctx,
		credInfo.Org.DID(),
		credInfo.Subject,
		spaceURI,
		fgastore.RelationSpaceOwner,
	)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check owner permission: %w", err))
		return
	}
	if !authorized {
		// TODO: we don't know if they're not authorize because they're not a member or
		// because they don't have the right role. assume worst case and return not found
		// need to return a reason from authorize
		httpx.WriteSpaceNotFound(ctx, w, fmt.Errorf("not authorized to delete space"))
		return
	}
	err = s.store.DeleteSpace(ctx, spaceURI)
	if errors.Is(err, ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(r.Context(), w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("delete space: %w", err))
		return
	}
}

func (s *Server) GetDelegationToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.oauth)).Validate(w, r)
	if !ok {
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, r.URL.Query().Get("space"), "space")
	if !ok {
		return
	}
	kid := "#atproto"
	privKey, err := s.hive.PrivateKeyForDID(ctx, credInfo.Subject)
	if errors.Is(err, identity.ErrDIDNotFound) {
		privKey = s.hostKey
		kid = "#habitat"
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get private key: %w", err))
		return
	}
	token, err := new(jwt.Token{
		Method: jwt.GetSigningMethod("ES256K"),
		Claims: jwt.MapClaims{
			"iss": credInfo.Subject,
			"sub": space.String(),
			"aud": space.SpaceOwner().String() + "#atproto_space_host",
			"iat": time.Now().Unix(),
			"exp": time.Now().Add(time.Minute).Unix(),
			"jti": utils.RandomNonce(16),
		},
		Header: map[string]any{
			"typ": "atproto-space-delegation+jwt",
			"kid": kid,
			"alg": "ES256K",
		},
	}).SignedString(privKey)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("sign token: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceGetDelegationTokenOutput{
		Token: token,
	})
}

func (s *Server) GetSpaceCredential(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceGetSpaceCredentialInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	if input.ClientAttestation != "" {
		httpx.WriteNotSupported(ctx, w, "client attestation is not yet supported")
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	if _, ok = authn.NewValidator(authn.WithAuthMethods(s.delegation), authn.WithSpace(spaceURI)).
		Validate(w, r); !ok {
		return
	}
	kid := "#atproto"
	privKey, err := s.hive.PrivateKeyForDID(ctx, spaceURI.SpaceOwner())
	if errors.Is(err, identity.ErrDIDNotFound) {
		privKey = s.hostKey
		kid = "#atproto_space_host"
	} else if err != nil {
		httpx.WriteSpaceNotFound(ctx, w, fmt.Errorf("failed to get host private key: %w", err))
		return
	}
	token, err := new(jwt.Token{
		Method: jwt.GetSigningMethod("ES256K"),
		Claims: jwt.MapClaims{
			"iss": spaceURI.SpaceOwner(),
			"sub": spaceURI,
			"iat": jwt.NewNumericDate(time.Now()),
			"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
			"jti": utils.RandomNonce(16),
		},
		Header: map[string]any{
			"typ": "atproto-space-credential+jwt",
			"kid": kid,
			"alg": "ES256K",
		},
	}).SignedString(privKey)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to sign token: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: token})
}
