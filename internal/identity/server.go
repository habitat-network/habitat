package identity

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/did"
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
	directory     identity.Directory
	oauth         authn.Method
	orgStore      org.Store
	pdsForwarding *forwarding.PDSForwarding
	domain        string
}

// NewServer constructs the hive HTTP server. The OAuth method is required to
// authenticate the caller for endpoints that mint things using the identity's
// signing key (e.g. com.atproto.server.getServiceAuth).
func NewServer(
	hive hive.Hive,
	oauth authn.Method,
	orgStore org.Store,
	pdsForwarding *forwarding.PDSForwarding,
	domain string,
) (*Server, error) {
	return &Server{
		hive:          hive,
		directory:     NewWrappedDirectory(hive, identity.DefaultDirectory()),
		oauth:         oauth,
		orgStore:      orgStore,
		pdsForwarding: pdsForwarding,
		domain:        domain,
	}, nil
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

	var ttl *time.Duration
	if expStr := r.URL.Query().Get("exp"); expStr != "" {
		expUnix, err := strconv.ParseInt(expStr, 10, 64)
		if err != nil {
			httpx.WriteInvalidRequest(ctx, w, "invalid exp", err)
			return
		}
		ttl = new(time.Until(time.Unix(expUnix, 0)))
	}

	var lxm *syntax.NSID
	if lxmStr := r.URL.Query().Get("lxm"); lxmStr != "" {
		parsed, ok := httpx.ParseNSIDInput(ctx, w, lxmStr, "lxm")
		if !ok {
			return
		}
		lxm = &parsed
	}

	privKey, err := s.hive.PrivateKeyForDID(ctx, credInfo.Subject)
	if errors.Is(err, identity.ErrDIDNotFound) {
		s.pdsForwarding.ServeHTTP(w, r)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("fetching signing key: %w", err))
		return
	}
	token, err := utils.ServiceAuthToken(privKey, credInfo.Subject, aud, lxm, ttl)
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
	reqDID, err := syntax.ParseDID("did:web:" + effectiveHost(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	ident, err := s.hive.LookupDID(r.Context(), reqDID)
	// TODO: better status codes dependening on the identity.Err type
	if err != nil {
		http.NotFound(w, r)
		return
	}
	did.NewHandler(ident).ServeHTTP(w, r)
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

// ResolveDID implements com.atproto.identity.resolveDid.
func (s *Server) ResolveDID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	did, ok := httpx.ParseDIDInput(ctx, w, r.URL.Query().Get("did"), "did")
	if !ok {
		return
	}
	ident, err := s.directory.LookupDID(ctx, did)
	if errors.Is(err, identity.ErrDIDNotFound) {
		httpx.WriteError(ctx, w, "DidNotFound", "DID not found", http.StatusNotFound)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("resolving DID: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, atproto.IdentityResolveDid_Output{
		DidDoc: s.overriddenDidDoc(ident),
	})
}

// ResolveHandle implements com.atproto.identity.resolveHandle.
func (s *Server) ResolveHandle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	handleStr := r.URL.Query().Get("handle")
	if handleStr == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required parameter: handle", nil)
		return
	}
	handle, err := syntax.ParseHandle(handleStr)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid handle", err)
		return
	}
	ident, err := s.directory.LookupHandle(ctx, handle)
	if errors.Is(err, identity.ErrHandleNotFound) {
		httpx.WriteError(ctx, w, "HandleNotFound", "handle not found", http.StatusNotFound)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("resolving handle: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, atproto.IdentityResolveHandle_Output{
		Did: ident.DID.String(),
	})
}

// ResolveIdentity implements com.atproto.identity.resolveIdentity.
func (s *Server) ResolveIdentity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identifier := r.URL.Query().Get("identifier")
	if identifier == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required parameter: identifier", nil)
		return
	}
	atid, err := syntax.ParseAtIdentifier(identifier)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid identifier", err)
		return
	}
	ident, err := s.directory.Lookup(ctx, atid)
	if errors.Is(err, identity.ErrDIDNotFound) {
		httpx.WriteError(ctx, w, "DidNotFound", "DID not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, identity.ErrHandleNotFound) {
		httpx.WriteError(ctx, w, "HandleNotFound", "handle not found", http.StatusNotFound)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("resolving identity: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, atproto.IdentityDefs_IdentityInfo{
		Did:    ident.DID.String(),
		Handle: ident.Handle.String(),
		DidDoc: s.overriddenDidDoc(ident),
	})
}

func (s *Server) overriddenDidDoc(ident *identity.Identity) identity.DIDDocument {
	b := did.New(ident.DID).AlsoKnownAs(ident.AlsoKnownAs...)
	for k, vm := range ident.Keys {
		b.VerificationMethod(
			fmt.Sprintf("%s#%s", ident.DID, k),
			vm.Type,
			ident.DID.String(),
			vm.PublicKeyMultibase,
		)
	}
	return b.ATProtoPDS("https://" + s.domain).Build().DIDDocument()
}
