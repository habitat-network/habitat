package privi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/api/agnostic"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/eagraf/habitat-new/core/permissions"
	"github.com/eagraf/habitat-new/internal/bffauth"
	"github.com/eagraf/habitat-new/internal/node/api"
	"github.com/rs/zerolog/log"
)

type PutRecordRequest struct {
	Input   *agnostic.RepoPutRecord_Input
	Encrypt bool `json:"encrypt"`
}

type Server struct {
	inner *store
	// Used to figure out where to route requests given a DID
	habitatResolver func(string) string
	// Used for resolving handles -> did, did -> PDS
	dir identity.Directory
	// The local pds host this server is tied to
	localPDSHost string
	// for habitat server-to-server communication
	bffClient bffauth.Client
	bffServer bffauth.Server
}

func NewServer(localPDSHost string, habitatResolver func(string) string, enc Encrypter, bffClient bffauth.Client, bffServer bffauth.Server, permStore permissions.Store) *Server {
	return &Server{
		inner: &store{
			e:           enc,
			permissions: permStore,
		},
		habitatResolver: habitatResolver,
		dir:             identity.DefaultDirectory(),
		localPDSHost:    localPDSHost,
		bffClient:       bffClient,
		bffServer:       bffServer,
	}
}

// PutRecord puts a potentially encrypted record (see s.inner.putRecord)
func (s *Server) PutRecord(authInfo *xrpc.AuthInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req PutRecordRequest
		slurp, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		err = json.Unmarshal(slurp, &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		xrpcClient := &xrpc.Client{
			Host: s.localPDSHost,
			Auth: authInfo,
		}
		out, err := s.inner.putRecord(r.Context(), xrpcClient, req.Input, req.Encrypt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		slurp, err = json.Marshal(out)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if _, err := w.Write(slurp); err != nil {
			log.Err(err).Msgf("error sending response for PutRecord request")
		}
	}
}

// Find desired did
// if other did, forward request there
// if our own did,
// --> if authInfo matches then fulfill the request
// --> otherwise verify requester's token via bff auth --> if they have permissions via permission store --> fulfill request

// GetRecord gets a potentially encrypted record (see s.inner.getRecord)
func (s *Server) GetRecord(authInfo *xrpc.AuthInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := url.Parse(r.URL.String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		cid := u.Query().Get("cid")
		collection := u.Query().Get("collection")
		repo := u.Query().Get("repo")
		rkey := u.Query().Get("rkey")

		// Try handling both handles and dids
		atid, err := syntax.ParseAtIdentifier(repo)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		id, err := s.dir.Lookup(r.Context(), *atid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		targetDID := id.DID.String()
		callerDID := "" /* unpopulated when unknown */
		var out *agnostic.RepoGetRecord_Output
		// If trying to get data from a PDS not managed by habitat
		if id.PDSEndpoint() != s.localPDSHost {
			// get bff token
			token, err := s.bffClient.GetToken(targetDID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Wack -- we're overloading AccessJwt to also pass around habitat managed tokens
			// Do this for ease for now so i can re-use xrpc client with PDS notions of auth but with Habitat notions of auth
			cli := &xrpc.Client{
				Auth: &xrpc.AuthInfo{
					AccessJwt: token,
				},
				Host: s.habitatResolver(targetDID),
			}
			// TODO: do i need to set an explicit header
			out, err = agnostic.RepoGetRecord(r.Context(), cli, cid, collection, targetDID, rkey) // nolint:staticcheck
		} else {
			// Wack -- whenever we are serving a request from another habitat node, only authInfo.accessJwt is populated
			// So in this case we validate the token.
			// If the request is coming from a requesting did served by this pds, then simply pass through to getRecord
			if authInfo.Did == "" || authInfo.Did != id.DID.String() {
				callerDID, err = s.bffServer.ValidateToken(authInfo.AccessJwt)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			cli := &xrpc.Client{
				Auth: authInfo,
				Host: s.localPDSHost,
			}
			// Local: call inner.getRecord
			out, err = s.inner.getRecord(r.Context(), cli, cid, collection, targetDID, rkey, callerDID) // nolint:staticcheck
		}
		if errors.Is(err, ErrUnauthorized) {
			http.Error(w, ErrUnauthorized.Error(), http.StatusForbidden)
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		slurp, err := json.Marshal(out)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := w.Write(slurp); err != nil {
			log.Err(err).Msgf("error sending response for GetRecord request")
		}
	}
}

// This creates the xrpc.Client to use in the inner privi requests
// TODO: this should actually pull out the requested did from the url param or input and re-direct there. (Potentially move this lower into the fns themselves).
// This would allow for requesting to any pds through these routes, not just the one tied to this habitat node.
func (s *Server) pdsAuthMiddleware(next func(authInfo *xrpc.AuthInfo) http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, err := getAuthInfo(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		next(auth).ServeHTTP(w, r)
	})
}

// HACK: trust did
func getAuthInfo(r *http.Request) (*xrpc.AuthInfo, error) {
	accessJwt := ""
	authHeader := r.Header.Get("Authorization")
	if len(authHeader) > 7 {
		accessJwt = authHeader[7:]
	}
	did := ""
	for _, cookie := range r.Cookies() {
		if cookie.Name == "access_token" {
			accessJwt = cookie.Value
		} else if cookie.Name == "did" {
			did = cookie.Value
		}
	}
	return &xrpc.AuthInfo{
		AccessJwt: accessJwt,
		Did:       did,
	}, nil
}

func (s *Server) GetRoutes() []api.Route {
	return []api.Route{
		api.NewBasicRoute(
			http.MethodPost,
			"/xrpc/com.habitat.putRecord",
			s.pdsAuthMiddleware(s.PutRecord),
		),
		api.NewBasicRoute(
			http.MethodGet,
			"/xrpc/com.habitat.getRecord",
			s.pdsAuthMiddleware(s.GetRecord),
		),
	}
}
