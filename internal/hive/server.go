package hive

import (
	"encoding/json"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Serve DID docs and handle --> did mappings.
// Does not serve the MintIdentity endpoint.
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

type didDocWithContext struct {
	Context []string `json:"@context"`
	identity.DIDDocument
}

var (
	didCtx = []string{"https://www.w3.org/ns/did/v1", "https://w3id.org/security/multikey/v1", "https://w3id.org/security/suites/secp256k1-2019/v1"}
)

// Serve DID Doc ( satisfy /{did}/.well-known/did.json )
func (s *Server) ServeDIDDoc(w http.ResponseWriter, r *http.Request) {
	did, err := syntax.ParseDID("did:web:" + r.Host)
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

	doc := didDocWithContext{
		Context:     didCtx,
		DIDDocument: ident.DIDDocument(),
	}

	w.Header().Set("Content-Type", "application/did+ld+json")
	w.Header().Set("Cache-Control", "max-age=3600")
	err = json.NewEncoder(w).Encode(doc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
