package hive

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type Server struct {
	hive Hive
}

func NewServer(hive Hive) (*Server, error) {
	return &Server{hive: hive}, nil
}

var (
	ErrWrongDomain = errors.New("this identity is not hosted by the server")
)

// returns the identifiery from the host
func (s *Server) parseIdentifier(host string) (syntax.AtIdentifier, error) {

	suffix := "." + s.hive.MemberDomain()
	if !strings.HasSuffix(host, suffix) {
		return "", ErrWrongDomain
	}

	return syntax.ParseAtIdentifier(host)
}

// ServeHTTP routes to ServeDIDDoc or ServeHandle based on the subdomain shape.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := s.parseIdentifier(r.Host)
	if errors.Is(err, ErrWrongDomain) {
		http.NotFound(w, r)
		return
	}
	if opaqueIDPattern.MatchString(id.String()) {
		did, err := id.AsDID()
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		s.ServeDIDDoc(w, r, did)
	} else {
		handle, err := id.AsHandle()
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		s.ServeHandle(w, r, handle)
	}
}

// Serve handle DID ( satisfy /.well-known/atproto-did )
func (s *Server) ServeHandle(w http.ResponseWriter, r *http.Request, handle syntax.Handle) {
	ident, err := s.hive.LookupHandle(r.Context(), handle)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, ident.DID.String())
}

// Serve DID Doc ( satisfy /.well-known/did.json )
func (s *Server) ServeDIDDoc(w http.ResponseWriter, r *http.Request, did syntax.DID) {
	ident, err := s.hive.LookupDID(r.Context(), did)
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
