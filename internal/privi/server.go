package privi

import (
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/eagraf/habitat-new/api/habitat"
	"github.com/eagraf/habitat-new/internal/node/api"
	"github.com/eagraf/habitat-new/internal/permissions"
	"github.com/eagraf/habitat-new/internal/utils"
	"github.com/gorilla/schema"
	"github.com/rs/zerolog/log"
)

type Server struct {
	// TODO: allow privy server to serve many stores, not just one user
	store *store
	// Used for resolving handles -> did, did -> PDS
	dir identity.Directory
	// TODO: should this really live here?
	repo *sqliteRepo
}

// NewServer returns a privi server.
func NewServer(perms permissions.Store, repo *sqliteRepo) *Server {
	server := &Server{
		store: newStore(perms, repo),
		dir:   identity.DefaultDirectory(),
		repo:  repo,
	}
	return server
}

var formDecoder = schema.NewDecoder()

// PutRecord puts a potentially encrypted record (see s.inner.putRecord)
func (s *Server) PutRecord(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := s.getAuthedUser(w, r)
	if !ok {
		return
	}
	var req habitat.NetworkHabitatRepoPutRecordInput
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusBadRequest)
		return
	}

	atid, err := syntax.ParseAtIdentifier(req.Repo)
	if err != nil {
		utils.LogAndHTTPError(w, err, "unmarshalling request", http.StatusBadRequest)
		return
	}

	ownerId, err := s.dir.Lookup(r.Context(), *atid)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parsing at identifier", http.StatusBadRequest)
		return
	}

	if ownerId.DID.String() != callerDID.String() {
		utils.LogAndHTTPError(w, err, "only owner can put record", http.StatusUnauthorized)
		return
	}

	var rkey string
	if req.Rkey == "" {
		rkey = uuid.NewString()
	} else {
		rkey = req.Rkey
	}

	v := true
	err = s.store.putRecord(ownerId.DID.String(), req.Collection, req.Record, rkey, &v)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			fmt.Sprintf("putting record for did %s", ownerId.DID.String()),
			http.StatusInternalServerError,
		)
		return
	}

	if _, err := w.Write([]byte("OK")); err != nil {
		log.Err(err).Msgf("error sending response for PutRecord request")
	}
}

// Find desired did
// if other did, forward request there
// if our own did,
// --> if authInfo matches then fulfill the request
// --> otherwise verify requester's token via bff auth --> if they have permissions via permission store --> fulfill request

// GetRecord gets a potentially encrypted record (see s.inner.getRecord)
func (s *Server) GetRecord(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := s.getAuthedUser(w, r)
	if !ok {
		return
	}
	var params habitat.NetworkHabitatRepoGetRecordParams
	err := formDecoder.Decode(&params, r.URL.Query())
	if err != nil {
		utils.LogAndHTTPError(w, err, "parsing url", http.StatusBadRequest)
		return
	}

	// Try handling both handles and dids
	atid, err := syntax.ParseAtIdentifier(params.Repo)
	if err != nil {
		// TODO: write helpful message
		utils.LogAndHTTPError(w, err, "parsing at identifier", http.StatusBadRequest)
		return
	}

	id, err := s.dir.Lookup(r.Context(), *atid)
	if err != nil {
		// TODO: write helpful message
		utils.LogAndHTTPError(w, err, "identity lookup", http.StatusBadRequest)
		return
	}

	targetDID := id.DID
	record, err := s.store.getRecord(params.Collection, params.Rkey, targetDID, callerDID)
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting record", http.StatusInternalServerError)
		return
	}

	if json.NewEncoder(w).Encode(record) != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) getAuthedUser(w http.ResponseWriter, r *http.Request) (did syntax.DID, ok bool) {
	did, err := s.getCaller(r)
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting caller did", http.StatusForbidden)
		return "", false
	}

	return did, true
}

func (s *Server) UploadBlob(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := s.getAuthedUser(w, r)
	if !ok {
		return
	}
	mimeType := r.Header.Get("Content-Type")
	if mimeType == "" {
		utils.LogAndHTTPError(
			w,
			fmt.Errorf("no mimetype specified"),
			"no mimetype specified",
			http.StatusInternalServerError,
		)
		return
	}

	bytes, err := io.ReadAll(r.Body)
	if err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusInternalServerError)
		return
	}

	blob, err := s.repo.uploadBlob(string(callerDID), bytes, mimeType)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"error in repo.uploadBlob",
			http.StatusInternalServerError,
		)
		return
	}

	out := habitat.NetworkHabitatRepoUploadBlobOutput{
		Blob: blob,
	}
	err = json.NewEncoder(w).Encode(out)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"error encoding json output",
			http.StatusInternalServerError,
		)
		return
	}
}

// HACK: trust did
func (s *Server) getCaller(r *http.Request) (syntax.DID, error) {
	authHeader := r.Header.Get("Authorization")
	token := strings.Split(authHeader, "Bearer ")[1]
	jwt.RegisterSigningMethod("ES256K", func() jwt.SigningMethod {
		return &SigningMethodSecp256k1{
			alg:      "ES256K",
			hash:     crypto.SHA256,
			toOutSig: toES256K, // R || S
			sigLen:   64,
		}
	})
	jwtToken, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
		did, err := t.Claims.GetIssuer()
		if err != nil {
			return nil, err
		}
		id, err := s.dir.LookupDID(r.Context(), syntax.DID(did))
		if err != nil {
			return "", errors.Join(errors.New("failed to lookup identity"), err)
		}
		return id.PublicKey()
	}, jwt.WithValidMethods([]string{"ES256K"}), jwt.WithoutClaimsValidation())
	if err != nil {
		return "", err
	}
	if jwtToken == nil {
		return "", fmt.Errorf("jwtToken is nil")
	}
	did, err := jwtToken.Claims.GetIssuer()
	if err != nil {
		return "", err
	}
	return syntax.DID(did), err
}

func (s *Server) ListPermissions(w http.ResponseWriter, r *http.Request) {
	callerDID, err := s.getCaller(r)
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting caller did", http.StatusForbidden)
		return
	}

	permissions, err := s.store.permissions.ListReadPermissionsByLexicon(callerDID.String())
	if err != nil {
		utils.LogAndHTTPError(w, err, "list permissions from store", http.StatusInternalServerError)
		return
	}

	err = json.NewEncoder(w).Encode(permissions)
	if err != nil {
		utils.LogAndHTTPError(w, err, "json marshal response", http.StatusInternalServerError)
		log.Err(err).Msgf("error sending response for ListPermissions request")
		return
	}
}

type editPermissionRequest struct {
	DID     string `json:"did"`
	Lexicon string `json:"lexicon"`
}

func (s *Server) AddPermission(w http.ResponseWriter, r *http.Request) {
	req := &editPermissionRequest{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}
	callerDID, err := s.getCaller(r)
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting caller did", http.StatusForbidden)
		return
	}

	err = s.store.permissions.AddLexiconReadPermission(req.DID, callerDID.String(), req.Lexicon)
	if err != nil {
		utils.LogAndHTTPError(w, err, "adding permission", http.StatusInternalServerError)
		return
	}
}

func (s *Server) RemovePermission(w http.ResponseWriter, r *http.Request) {
	req := &editPermissionRequest{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}
	callerDID, err := s.getCaller(r)
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting caller did", http.StatusForbidden)
		return
	}

	err = s.store.permissions.RemoveLexiconReadPermission(req.DID, callerDID.String(), req.Lexicon)
	if err != nil {
		utils.LogAndHTTPError(w, err, "removing permission", http.StatusInternalServerError)
		return
	}
}

func (s *Server) GetRoutes() []api.Route {
	return []api.Route{
		api.NewBasicRoute(
			http.MethodPost,
			"/xrpc/com.habitat.putRecord",
			s.PutRecord,
		),
		api.NewBasicRoute(
			http.MethodGet,
			"/xrpc/com.habitat.getRecord",
			s.GetRecord,
		),
		api.NewBasicRoute(http.MethodPost, "/xrpc/com.habitat.addPermission", s.AddPermission),
		api.NewBasicRoute(
			http.MethodPost,
			"/xrpc/com.habitat.removePermission",
			s.RemovePermission,
		),
		api.NewBasicRoute(http.MethodGet, "/xrpc/com.habitat.listPermissions", s.ListPermissions),
	}
}
