package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/org"
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

// Server serves DID docs and handle --> did mappings.
// Does not serve the MintIdentity endpoint.
type Server struct {
	hive          hive.Hive
	oauth         authn.Method
	orgStore      org.Store
	pdsForwarding *forwarding.PDSForwarding
}

// NewServer constructs the hive HTTP server. The OAuth method is required to
// authenticate the caller for endpoints that mint things using the identity's
// signing key (e.g. com.atproto.server.getServiceAuth).
func NewServer(
	hive hive.Hive,
	oauth authn.Method,
	orgStore org.Store,
	pdsForwarding *forwarding.PDSForwarding,
) (*Server, error) {
	return &Server{hive: hive, oauth: oauth, orgStore: orgStore, pdsForwarding: pdsForwarding}, nil
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
	ctx := r.Context()
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth),
	).Validate(w, r)
	if !ok {
		return
	}

	aud := r.URL.Query().Get("aud")
	if aud == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required parameter: aud", nil)
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
			httpx.WriteInvalidRequest(ctx, w, "invalid exp", err)
			return
		}
		ttl = time.Until(time.Unix(expUnix, 0))
		if ttl <= 0 {
			httpx.WriteInvalidRequest(ctx, w, "exp is in the past", nil)
			return
		}
		if ttl > maxTTL {
			ttl = maxTTL
		}
	}

	var lxm *syntax.NSID
	if lxmStr := r.URL.Query().Get("lxm"); lxmStr != "" {
		parsed, ok := httpx.ParseNSIDInput(ctx, w, lxmStr, "lxm")
		if !ok {
			return
		}
		lxm = &parsed
	}

	token, err := s.hive.SignJWT(ctx, credInfo.Subject,
		map[string]any{},
		jwt.MapClaims{
			"exp": jwt.NewNumericDate(time.Now().Add(time.Minute)),
			"iat": jwt.NewNumericDate(time.Now()),
			"iss": credInfo.Subject,
			"aud": aud,
			"jti": utils.RandomNonce(16),
			"lxm": lxm,
		},
	)
	if errors.Is(err, identity.ErrDIDNotFound) {
		s.pdsForwarding.ServeHTTP(w, r)
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("signing service auth: %w", err))
		return
	}

	httpx.WriteJSON(ctx, w, struct {
		Token string `json:"token"`
	}{Token: token})
}

// For now, DIDs and handles are public. Eventually, we can make them private behind an
// auth boundary, to not leak info about who is in an org.

// Serve DID Doc ( satisfy /{did}/.well-known/did.json )
func (s *Server) ServeDIDDoc(w http.ResponseWriter, r *http.Request) {
	// Get the requested DID
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
	}
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
		http.Error(
			w,
			"internal error",
			http.StatusInternalServerError,
		) // don't leak whether the DID exists or not
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(ident.DID.String()))
}
