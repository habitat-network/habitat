package hive

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/utils"
)

const HabitatHostHeader = "Habitat-Host"

// effectiveHost returns the Habitat-Host header value if present,
// otherwise falls back to the request's Host field.
func effectiveHost(r *http.Request) string {
	if h := r.Header.Get(HabitatHostHeader); h != "" {
		return h
	}
	return r.Host
}

// Serve DID docs and handle --> did mappings.
// Does not serve the MintIdentity endpoint.
type Server struct {
	hive  Hive
	oauth authn.Method
}

// NewServer constructs the hive HTTP server. The OAuth method is required to
// authenticate the caller for endpoints that mint things using the identity's
// signing key (e.g. com.atproto.server.getServiceAuth).
func NewServer(hive Hive, oauth authn.Method) (*Server, error) {
	return &Server{hive: hive, oauth: oauth}, nil
}

// Serve handle DID ( satisfy /{handle}/.well-known/atproto-did )
func (s *Server) ServeHandle(w http.ResponseWriter, r *http.Request) {
	handle, err := syntax.ParseHandle(effectiveHost(r))
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

var didCtx = []string{
	"https://www.w3.org/ns/did/v1",
	"https://w3id.org/security/multikey/v1",
	"https://w3id.org/security/suites/secp256k1-2019/v1",
}

// GetServiceAuth implements com.atproto.server.getServiceAuth for habitat-hosted
// identities. Habitat owns the signing key registered in the identity's did:web
// doc, so it (not the upstream PDS) is what can mint atproto-compatible service
// auth JWTs. Downstream services verify the token by resolving the DID and
// fetching the same signing key, with no changes needed on their end.
func (s *Server) GetServiceAuth(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.oauth)
	if !ok {
		return
	}

	aud := r.URL.Query().Get("aud")
	if aud == "" {
		utils.WriteHTTPError(
			w,
			errors.New("missing required parameter: aud"),
			http.StatusBadRequest,
		)
		return
	}

	// exp is the unix-seconds expiration. Default to 60s to match the lexicon
	// default; cap at 30min to limit blast radius if a token leaks.
	const defaultTTL = 60 * time.Second
	const maxTTL = 30 * time.Minute
	ttl := defaultTTL
	if expStr := r.URL.Query().Get("exp"); expStr != "" {
		expUnix, err := strconv.ParseInt(expStr, 10, 64)
		if err != nil {
			utils.WriteHTTPError(w, fmt.Errorf("parsing exp: %w", err), http.StatusBadRequest)
			return
		}
		ttl = time.Until(time.Unix(expUnix, 0))
		if ttl <= 0 {
			utils.WriteHTTPError(w, errors.New("exp is in the past"), http.StatusBadRequest)
			return
		}
		if ttl > maxTTL {
			ttl = maxTTL
		}
	}

	var lxm *syntax.NSID
	if lxmStr := r.URL.Query().Get("lxm"); lxmStr != "" {
		parsed, err := syntax.ParseNSID(lxmStr)
		if err != nil {
			utils.WriteHTTPError(w, fmt.Errorf("parsing lxm: %w", err), http.StatusBadRequest)
			return
		}
		lxm = &parsed
	}

	token, err := s.hive.SignServiceAuth(r.Context(), callerDID, aud, ttl, lxm)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"signing service auth",
			http.StatusInternalServerError,
		)
		return
	}

	if err := json.NewEncoder(w).Encode(struct {
		Token string `json:"token"`
	}{Token: token}); err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"encoding response",
			http.StatusInternalServerError,
		)
		return
	}
}

// Serve DID Doc ( satisfy /{did}/.well-known/did.json )
func (s *Server) ServeDIDDoc(w http.ResponseWriter, r *http.Request) {
	did, err := syntax.ParseDID("did:web:" + effectiveHost(r))
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
		return
	}
}
