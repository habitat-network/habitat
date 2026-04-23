package hive

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/utils"
)

type Server struct {
	hive Hive
}

func NewServer(hive Hive) (*Server, error) {
	return &Server{hive: hive}, nil
}

// Serve handle DID ( satisfy /{handle}/.well-known/atproto-did )
func (s *Server) ServeHandle(w http.ResponseWriter, r *http.Request) {
	handle, err := syntax.ParseHandle(r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	ident, err := s.hive.LookupHandle(r.Context(), handle)
	// TODO: better status codes dependening on the identity.Err type
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(ident.DID.String()))
}

// Serve DID Doc ( satisfy /{did}/.well-known/did.json )
func (s *Server) ServeDIDDoc(w http.ResponseWriter, r *http.Request) {
	did, err := syntax.ParseDID("did:web:" + r.Host)
	fmt.Println("did", did)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	ident, err := s.hive.LookupDID(r.Context(), did)
	// TODO: better status codes dependening on the identity.Err type
	if err != nil {
		http.NotFound(w, r)
		return
	}

	doc := ident.DIDDocument()

	w.Header().Set("Content-Type", "application/did+ld+json")
	w.Header().Set("Cache-Control", "max-age=3600")
	err = json.NewEncoder(w).Encode(doc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) MintIdentity(w http.ResponseWriter, r *http.Request) {
	// TODO: authz here with a token via link or something so only blessed people can mint identity

	var req habitat.NetworkHabitatHiveMintIdentityInput
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusBadRequest)
		return
	}

	err = s.hive.MintIdentity(req.Handle)
	if err != nil {
		utils.LogAndHTTPError(w, err, "minting identity", http.StatusInternalServerError)
		return
	}
}
