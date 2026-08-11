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

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
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
	store     Store
	fga       fgastore.Store
	validator authn.RequestValidator
	decoder   *schema.Decoder
	orgStore  org.Store
	commit    *spacecommit.Authority
	hive      hive.Hive
	blobs     BlobStore
	hostKey   atcrypto.PrivateKey
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
	validator authn.RequestValidator,
	orgStore org.Store,
	hostPrivateKey atcrypto.PrivateKey,
	hive hive.Hive,
	blobs BlobStore,
) *Server {
	return &Server{
		store:     store,
		fga:       fga,
		decoder:   schema.NewDecoder(),
		orgStore:  orgStore,
		commit:    spacecommit.NewAuthority(hostPrivateKey, hive),
		hive:      hive,
		blobs:     blobs,
		hostKey:   hostPrivateKey,
		validator: validator,
	}
}

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
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
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
	err := s.store.AddMember(r.Context(), spaceURI, memberDID, SpaceAccessWrite)
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
	err := s.store.RemoveMember(r.Context(), spaceURI, memberDID)
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
	_, ok = s.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
		authn.WithSpace(spaceURI, fgastore.RelationSpaceReader),
	).Validate(w, r)
	if !ok {
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
	var input habitat.NetworkHabitatSpacePutRecordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
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
	credInfo, ok := s.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
		authn.WithSpace(spaceURI, fgastore.RelationSpaceWriter),
	).Validate(w, r)
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

// CreateRecord handles network.habitat.space.createRecord: like PutRecord,
// but fails rather than overwriting when a record already exists at the
// given collection/rkey, per the proposal's PDS method table.
func (s *Server) CreateRecord(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceCreateRecordInput
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
	credInfo, ok := s.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
		authn.WithSpace(spaceURI, fgastore.RelationSpaceWriter),
	).Validate(w, r)
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
	recordURI, cid, err := s.store.CreateRecord(
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
	} else if errors.Is(err, ErrRecordAlreadyExists) {
		httpx.WriteError(
			ctx, w, "RecordAlreadyExists", "a record already exists at this collection/rkey",
			http.StatusConflict,
		)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("create record: %w", err))
		return
	}
	httpx.WriteJSON(r.Context(), w, habitat.NetworkHabitatSpaceCreateRecordOutput{
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
	_, ok = s.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
		authn.WithSpace(spaceURI, fgastore.RelationSpaceReader),
	).Validate(w, r)
	if !ok {
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
	_, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
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
	_, ok = s.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
		authn.WithSpace(spaceURI, fgastore.RelationSpaceReader),
	).Validate(w, r)
	if !ok {
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
	_, ok = s.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
		authn.WithSpace(spaceURI, fgastore.RelationSpaceReader),
	).Validate(w, r)
	if !ok {
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
	_, ok = s.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
		authn.WithSpace(spaceURI, fgastore.RelationSpaceReader),
	).Validate(w, r)
	if !ok {
		return
	}
	repoDID, ok := httpx.ParseDIDInput(ctx, w, params.Repo, "repo")
	if !ok {
		return
	}
	// Read the repo's head and its record blocks as of the same point (see
	// RepoSnapshot), so the commit signed below always agrees with the blocks
	// the CAR actually carries. An empty repo has no state to recover, so it
	// reports as not found.
	rev, hash, blocks, found, err := s.store.RepoSnapshot(ctx, spaceURI, repoDID)
	if errors.Is(err, ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("repo snapshot: %w", err))
		return
	}
	if !found {
		httpx.WriteRepoNotFound(ctx, w, errEmptyRepo)
		return
	}

	// The signed commit is the CAR's first root.
	commit, err := s.commit.Build(ctx, spaceURI, repoDID, rev, hash)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("sign repo head: %w", err))
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
	_, ok = s.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
		authn.WithSpace(spaceURI, fgastore.RelationSpaceReader),
	).Validate(w, r)
	if !ok {
		return
	}
	repoDID, ok := httpx.ParseDIDInput(ctx, w, params.Repo, "repo")
	if !ok {
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
		switch {
		case err == nil:
			// The ops and the head commit come from two separate reads, so a
			// write landing between them produces a commit describing state
			// these ops do not cover. A syncer folds the ops and compares its
			// hash against the commit, so that mismatch reads as divergence
			// and drops an otherwise healthy repo into full recovery. The
			// commit's rev names the state it covers: include it only when
			// that is exactly where these ops leave the syncer, and otherwise
			// omit it so the syncer takes another page and asks again.
			at := params.Since
			if len(records) > 0 {
				at = records[len(records)-1].Rev
			}
			if commit.Rev == at {
				output.Commit = commit
			}
		case errors.Is(err, errEmptyRepo):
			// Nothing has been written to the repo, so there is no head to
			// describe and nothing for a syncer to verify against.
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
	_, ok = s.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
		authn.WithSpace(spaceURI, fgastore.RelationSpaceReader),
	).Validate(w, r)
	if !ok {
		return
	}
	repoDID, ok := httpx.ParseDIDInput(ctx, w, params.Repo, "repo")
	if !ok {
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
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
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
		httpx.WriteInvalidRequest(ctx, w, "can't write to other repo", fmt.Errorf("wrong repo"))
		return
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
	err := s.store.DeleteSpace(ctx, spaceURI)
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
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
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
	token, err := utils.DelegationToken(privKey, credInfo.Subject, kid, space)
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
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	if _, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodDelegationToken),
		authn.WithSpace(spaceURI, fgastore.RelationSpaceReader),
	).Validate(w, r); !ok {
		return
	}
	// kid must name a verification method the signer's DID document actually
	// publishes, since a verifier resolves the space authority's DID document
	// and looks the fragment up as a verification method (not a service — the
	// former "#atproto_space_host" named a service and could never resolve).
	// A hive-managed author's DID document publishes only "#atproto" (see
	// hive.Hive.mintDID), matching the proposal's fallback rule when
	// "#atproto_space" is absent; the host's own DID document publishes
	// "#atproto_space" directly.
	kid := "#atproto"
	privKey, err := s.hive.PrivateKeyForDID(ctx, spaceURI.SpaceOwner())
	if errors.Is(err, identity.ErrDIDNotFound) {
		privKey = s.hostKey
		kid = "#atproto_space"
	} else if err != nil {
		httpx.WriteSpaceNotFound(ctx, w, fmt.Errorf("failed to get host private key: %w", err))
		return
	}
	token, err := utils.SpaceCredential(privKey, kid, spaceURI)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to sign token: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: token})
}
