package privi

import (
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/eagraf/habitat-new/internal/node/api"
	"github.com/eagraf/habitat-new/internal/permissions"
	"github.com/rs/zerolog/log"
)

type PutRecordRequest struct {
	Collection string         `json:"collection"`
	Repo       string         `json:"repo"`
	Rkey       string         `json:"rkey,omitempty"`
	Record     map[string]any `json:"record"`
}

type Server struct {
	// TODO: allow privy server to serve many stores, not just one user
	store *store
	// Used for resolving handles -> did, did -> PDS
	dir identity.Directory
	// TODO: should this really live here?
	repo repo
}

// NewServer returns a privi server.
func NewServer(perms permissions.Store, repo repo) *Server {
	server := &Server{
		store: newStore(perms, repo),
		dir:   identity.DefaultDirectory(),
		repo:  repo,
	}
	return server
}

// PutRecord puts a potentially encrypted record (see s.inner.putRecord)
func (s *Server) PutRecord(w http.ResponseWriter, r *http.Request) {
	var req PutRecordRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	atid, err := syntax.ParseAtIdentifier(req.Repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ownerId, err := s.dir.Lookup(r.Context(), *atid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
func (s *Server) GetRecord(callerDID syntax.DID) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := url.Parse(r.URL.String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		collection := u.Query().Get("collection")
		repo := u.Query().Get("repo")
		rkey := u.Query().Get("rkey")

		// Try handling both handles and dids
		atid, err := syntax.ParseAtIdentifier(repo)
		if err != nil {
			// TODO: write helpful message
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		id, err := s.dir.Lookup(r.Context(), *atid)
		if err != nil {
			// TODO: write helpful message
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		targetDID := id.DID
		out, err := s.store.getRecord(collection, rkey, targetDID, callerDID)
		if errors.Is(err, ErrUnauthorized) {
			http.Error(w, ErrUnauthorized.Error(), http.StatusForbidden)
			return
		} else if errors.Is(err, ErrRecordNotFound) {
			http.Error(w, ErrRecordNotFound.Error(), http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if _, err := w.Write(out); err != nil {
			log.Err(err).Msgf("error sending response for GetRecord request")
		}
	}
}

// This creates the xrpc.Client to use in the inner privi requests
// TODO: this should actually pull out the requested did from the url param or input and re-direct there. (Potentially move this lower into the fns themselves).
// This would allow for requesting to any pds through these routes, not just the one tied to this habitat node.
func (s *Server) PdsAuthMiddleware(next func(syntax.DID) http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		did, err := s.getCaller(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		next(did).ServeHTTP(w, r)
	})
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
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	permissions, err := s.store.permissions.ListReadPermissionsByLexicon(callerDID.String())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = json.NewEncoder(w).Encode(permissions)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	callerDID, err := s.getCaller(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	err = s.store.permissions.AddLexiconReadPermission(req.DID, callerDID.String(), req.Lexicon)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *Server) RemovePermission(w http.ResponseWriter, r *http.Request) {
	req := &editPermissionRequest{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	callerDID, err := s.getCaller(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	err = s.store.permissions.RemoveLexiconReadPermission(req.DID, callerDID.String(), req.Lexicon)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
			s.PdsAuthMiddleware(s.GetRecord),
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
