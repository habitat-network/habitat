package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/pkg/sap"
)

// habitatDIDHeader names the DID the caller wants the proxied request to be
// authenticated as. sap looks up the OAuth session it tracks for this DID.
const (
	habitatDIDHeader     = "Habitat-Did"
	habitatSessionHeader = "Habitat-Session"
)

// hopByHopHeaders are connection-scoped and must not be forwarded to pear per
// the HTTP/1.1 spec (RFC 7230 §6.1).
var hopByHopHeaders = []string{
	"Connection", "Transfer-Encoding", "Te", "Upgrade", "Keep-Alive",
}

type server struct {
	sap             *sap.Sap
	oauthClient     *oauth.ClientApp
	notifyValidator *auth.ServiceAuthValidator // verifies incoming notifyWrite deliveries

	// outboxPingPeriod/PongWait/WriteWait configure handleOutboxChannel's
	// liveness checks (see their defaults in websocket.go). Tests shrink
	// these directly on a *server instance rather than a shared package
	// var, so parallel tests exercising different timeouts don't race.
	outboxPingPeriod time.Duration
	outboxPongWait   time.Duration
	outboxWriteWait  time.Duration

	mu              sync.Mutex
	pendingReturnTo map[string]string // DID string -> return_to URL
	clientMetadata  ConfiguredClientMetadata

	// identityResolverURL is the base URL of the identity service sap
	// resolves identities through (the --identity-resolver flag), or empty
	// when sap uses the public network. Only a Habitat instance resolves
	// work emails, so email sign-in requires it.
	identityResolverURL string
	httpClient          *http.Client
}

// endpoint is sap's own public base URL (the same value passed as
// sap.Config.Endpoint) — it's both what sap registers with space hosts as
// its notifyWrite delivery address, and the audience space hosts sign into
// the service-auth JWT they deliver notifyWrite calls with.
func NewSapServer(
	sapInstance *sap.Sap,
	oauthClient *oauth.ClientApp,
	endpoint string,
	clientMetadata ConfiguredClientMetadata,
	identityResolverURL string,
) *server {
	return &server{
		sap:         sapInstance,
		oauthClient: oauthClient,
		notifyValidator: &auth.ServiceAuthValidator{
			Dir:      oauthClient.Dir,
			Audience: endpoint,
		},
		outboxPingPeriod: defaultOutboxPingPeriod,
		outboxPongWait:   defaultOutboxPongWait,
		outboxWriteWait:  defaultOutboxWriteWait,
		pendingReturnTo:  make(map[string]string),
		clientMetadata:   clientMetadata,

		identityResolverURL: strings.TrimSuffix(identityResolverURL, "/"),
		httpClient:          httpx.NewClient(),
	}
}

var (
	// errEmailSignInUnsupported is returned by resolveEmail when sap has no
	// identity resolver configured to resolve emails through.
	errEmailSignInUnsupported = errors.New("email sign-in requires an identity resolver")
	// errEmailNotFound is returned by resolveEmail when no identity exists
	// or can be provisioned for an email, i.e. its domain isn't mapped to
	// an org.
	errEmailNotFound = errors.New("no identity for email")
)

// resolveEmail resolves a work email to the DID provisioned for it through
// the identity resolver's com.atproto.identity.resolveIdentity, which a
// Habitat instance extends to accept work emails (minting an identity on first
// sight). indigo's apidir can't be used for this: its Lookup only takes an
// at-identifier.
func (s *server) resolveEmail(ctx context.Context, email emaildomain.Email) (syntax.DID, error) {
	if s.identityResolverURL == "" {
		return "", errEmailSignInUnsupported
	}
	u := s.identityResolverURL + "/xrpc/com.atproto.identity.resolveIdentity?" +
		url.Values{"identifier": {string(email)}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("build resolve request: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve email: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", errEmailNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve email: unexpected status %d", resp.StatusCode)
	}
	var body struct {
		DID string `json:"did"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode resolved identity: %w", err)
	}
	did, err := syntax.ParseDID(body.DID)
	if err != nil {
		return "", fmt.Errorf("resolved identity has invalid DID: %w", err)
	}
	return did, nil
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(r.Context(), w, map[string]string{"status": "ok"})
}

func (s *server) handleAddSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Handle   string `json:"handle"`
		ReturnTo string `json:"return_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// identifier is what's handed to StartAuthFlow. A work email isn't an
	// at-identifier, so StartAuthFlow can't take it directly: resolve it to
	// its provisioned DID first. The Habitat instance then routes that DID's
	// sign-in through its email domain's login method (e.g. Google).
	identifier := req.Handle
	if email, err := emaildomain.ParseEmail(req.Handle); err == nil {
		did, err := s.resolveEmail(r.Context(), email)
		switch {
		case errors.Is(err, errEmailSignInUnsupported):
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		case errors.Is(err, errEmailNotFound):
			http.Error(w, "email domain is not set up for sign-in", http.StatusNotFound)
			return
		case err != nil:
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		identifier = did.String()
	}

	// oauth.ClientApp.StartAuthFlow has no hook to carry caller state through
	// the OAuth state param and doesn't return the resolved DID, so when the
	// caller wants to be redirected back we resolve the identifier to a DID
	// ourselves (the same lookup StartAuthFlow performs internally) and stash
	// return_to keyed by that DID for handleOAuthCallback to pick up later.
	// Resolution failures here must not block the StartAuthFlow call below —
	// they just mean the caller won't get redirected back.
	if req.ReturnTo != "" {
		if atid, err := syntax.ParseAtIdentifier(identifier); err == nil {
			if ident, err := s.oauthClient.Dir.Lookup(r.Context(), atid); err == nil {
				s.mu.Lock()
				s.pendingReturnTo[ident.DID.String()] = req.ReturnTo
				s.mu.Unlock()
			}
		}
	}

	redirectURL, err := s.oauthClient.StartAuthFlow(r.Context(), identifier)
	if err != nil {
		http.Error(w, fmt.Sprintf("start auth flow: %s", err), http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodPost {
		httpx.WriteJSON(r.Context(), w, map[string]string{"redirect_url": redirectURL})
		return
	}
	w.Header().Set("Location", redirectURL)
	w.WriteHeader(http.StatusSeeOther)
}

func (s *server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.sap.Sessions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(r.Context(), w, map[string]any{"sessions": sessions})
}

// handleTrackSpace tells sap to start tracking a space it wouldn't discover
// through (did, sessionID)'s own crawl — e.g. one a caller just created —
// via Sap.TrackSpace: records space access, registers for push
// notifications, and syncs its repos immediately rather than waiting for
// the next crawl.
func (s *server) handleTrackSpace(w http.ResponseWriter, r *http.Request) {
	didStr := r.Header.Get(habitatDIDHeader)
	if didStr == "" {
		http.Error(w, "missing "+habitatDIDHeader+" header", http.StatusBadRequest)
		return
	}
	did, ok := httpx.ParseDIDInput(r.Context(), w, didStr, habitatDIDHeader)
	if !ok {
		return
	}
	// Optional: empty resolves fine under WithSingleSessionPerUser (the
	// session ID is ignored), and callers that don't track one can just omit
	// the header.
	sessionID := r.Header.Get(habitatSessionHeader)

	var req struct {
		Space string `json:"space"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	spaceURI, err := syntax.ParseURI(req.Space)
	if err != nil {
		http.Error(w, fmt.Sprintf("parse space: %s", err), http.StatusBadRequest)
		return
	}

	if err := s.sap.TrackSpace(r.Context(), spaceURI, did, sessionID); err != nil {
		http.Error(w, fmt.Sprintf("track space: %s", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleSpaceCredential returns a space credential for the space query param,
// and the space host it is valid against, via Sap.SpaceCredential — minted
// through whichever tracked session has recorded access to the space, so a
// caller with no session of its own for the space (e.g. chalk reading a doc's
// blob while handling an outbox webhook) can read the space's host directly.
func (s *server) handleSpaceCredential(w http.ResponseWriter, r *http.Request) {
	space, ok := httpx.ParseSpaceURIInput(r.Context(), w, r.URL.Query().Get("space"), "space")
	if !ok {
		return
	}
	cred, err := s.sap.SpaceCredential(r.Context(), space)
	if err != nil {
		http.Error(w, fmt.Sprintf("space credential: %s", err), http.StatusBadGateway)
		return
	}
	httpx.WriteJSON(r.Context(), w, map[string]string{
		"credential": cred.Token,
		"host":       cred.Host,
	})
}

// handleRecrawl retriggers a crawl for a session via Sap.Recrawl, discarding
// any progress from a previous crawl and re-running discovery from the top.
// Internal endpoint, for operators to unstick a session whose crawl is stuck
// or errored without waiting for the next periodic re-crawl. Sap.Recrawl
// schedules the crawl and returns immediately, so this always responds 202
// once scheduled rather than waiting for the crawl to finish.
func (s *server) handleRecrawl(w http.ResponseWriter, r *http.Request) {
	didStr := r.Header.Get(habitatDIDHeader)
	if didStr == "" {
		http.Error(w, "missing "+habitatDIDHeader+" header", http.StatusBadRequest)
		return
	}
	did, ok := httpx.ParseDIDInput(r.Context(), w, didStr, habitatDIDHeader)
	if !ok {
		return
	}
	// Optional: empty resolves fine under WithSingleSessionPerUser (the
	// session ID is ignored), and callers that don't track one can just omit
	// the header.
	sessionID := r.Header.Get(habitatSessionHeader)

	s.sap.Recrawl(r.Context(), did, sessionID)
	w.WriteHeader(http.StatusAccepted)
}

// handleNotifyWrite receives network.habitat.space.notifyWrite deliveries —
// the callback a space host makes (internal/notify.Deliverer) to every
// endpoint sap has registered via registerNotify (see pkg/sap/register) —
// and forwards them into Sap.NotifyWrite so the corresponding repo gets
// synced (and, for chalk's docs collection, lands in the outbox) right away
// instead of waiting for the next full crawl.
func (s *server) handleNotifyWrite(w http.ResponseWriter, r *http.Request) {
	tokenStr, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	nsid := syntax.NSID("network.habitat.space.notifyWrite")
	if _, err := s.notifyValidator.Validate(r.Context(), tokenStr, &nsid); err != nil {
		http.Error(w, fmt.Sprintf("invalid service auth: %s", err), http.StatusUnauthorized)
		return
	}

	var req struct {
		Space string       `json:"space"`
		Repo  string       `json:"repo"`
		Rev   string       `json:"rev"`
		Hash  atdata.Bytes `json:"hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	spaceURI, err := syntax.ParseURI(req.Space)
	if err != nil {
		http.Error(w, fmt.Sprintf("parse space: %s", err), http.StatusBadRequest)
		return
	}
	repoDID, ok := httpx.ParseDIDInput(r.Context(), w, req.Repo, "repo")
	if !ok {
		return
	}
	rev, err := syntax.ParseTID(req.Rev)
	if err != nil {
		http.Error(w, fmt.Sprintf("parse rev: %s", err), http.StatusBadRequest)
		return
	}

	if err := s.sap.NotifyWrite(r.Context(), spaceURI, repoDID, rev, req.Hash); err != nil {
		http.Error(w, fmt.Sprintf("notify write: %s", err), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(r.Context(), w, map[string]string{})
}

func (s *server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	sessionData, err := s.oauthClient.ProcessCallback(r.Context(), r.URL.Query())
	if err != nil {
		http.Error(w, fmt.Sprintf("process callback: %s", err), http.StatusInternalServerError)
		return
	}

	if err := s.sap.AddSession(
		r.Context(),
		sessionData.AccountDID,
		sessionData.SessionID,
	); err != nil {
		http.Error(w, fmt.Sprintf("save org: %s", err), http.StatusInternalServerError)
		return
	}

	slog.InfoContext(r.Context(), "org oauth complete", "did", sessionData.AccountDID)

	if s.redirectToReturnTo(w, r, sessionData.AccountDID.String()) {
		return
	}
	w.WriteHeader(http.StatusOK)
}

// redirectToReturnTo pops any pending return_to for did and, if present,
// redirects r's response there with the DID appended as a query param.
// Returns true if it wrote a response (caller must not write another one).
func (s *server) redirectToReturnTo(w http.ResponseWriter, r *http.Request, did string) bool {
	s.mu.Lock()
	returnTo, ok := s.pendingReturnTo[did]
	if ok {
		delete(s.pendingReturnTo, did)
	}
	s.mu.Unlock()
	if !ok {
		return false
	}
	target := fmt.Sprintf("%s?did=%s", returnTo, url.QueryEscape(did))
	http.Redirect(w, r, target, http.StatusSeeOther)
	return true
}

// handleProxy forwards an XRPC request to pear on behalf of a managed org,
// authenticating with the OAuth session sap tracks for the DID named in the
// Habitat-Did header. The path after /proxy/ is the XRPC method, forwarded to
// pear as /xrpc/<method> with the original method, query params, headers, and
// body preserved.
func (s *server) handleProxy(w http.ResponseWriter, r *http.Request) {
	didStr := r.Header.Get(habitatDIDHeader)
	if didStr == "" {
		http.Error(w, "missing "+habitatDIDHeader+" header", http.StatusBadRequest)
		return
	}
	did, ok := httpx.ParseDIDInput(r.Context(), w, didStr, habitatDIDHeader)
	if !ok {
		return
	}
	// Optional: empty resolves fine under WithSingleSessionPerUser (the
	// session ID is ignored), and callers that don't track one can just omit
	// the header.
	sessionID := r.Header.Get(habitatSessionHeader)
	sess, err := s.oauthClient.ResumeSession(r.Context(), did, sessionID)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf("no tracked session for %s: %s", did, err),
			http.StatusBadGateway,
		)
		return
	}

	nsid, ok := httpx.ParseNSIDInput(
		r.Context(),
		w,
		strings.TrimPrefix(r.URL.Path, "/proxy/"),
		"nsid",
	)
	if !ok {
		return
	}
	// Resolve the target against the org's Habitat host tracked in the OAuth
	// session; DoWithAuth attaches the access token but expects an absolute URL.
	target := sess.Data.HostURL + "/xrpc/" + nsid.String()
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	// Buffer the body into a *bytes.Reader rather than passing r.Body
	// through directly: DoWithAuth retries the request on a DPoP nonce
	// update or token refresh via req.GetBody, which net/http only
	// populates automatically for a handful of reusable body types (bytes,
	// strings buffers/readers) — not for an arbitrary io.Reader like the
	// incoming request's body, which read Close()s after the first attempt
	// and can't be replayed.
	var body io.Reader
	if r.Body != nil {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(
				w,
				fmt.Sprintf("read request body: %s", err),
				http.StatusInternalServerError,
			)
			return
		}
		body = bytes.NewReader(bodyBytes)
	}
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, target, body)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf("build forwarded request: %s", err),
			http.StatusInternalServerError,
		)
		return
	}

	// Clone the caller's headers, then scrub hop-by-hop headers, any headers
	// named in the Connection value, and the auth-related headers we replace.
	outReq.Header = r.Header.Clone()
	for _, h := range strings.Split(outReq.Header.Get("Connection"), ",") {
		outReq.Header.Del(strings.TrimSpace(h))
	}
	for _, h := range hopByHopHeaders {
		outReq.Header.Del(h)
	}
	outReq.Header.Del(habitatDIDHeader)
	outReq.Header.Del(habitatSessionHeader)
	outReq.Header.Del("Authorization")
	outReq.Header.Set("Habitat-Auth-Method", "oauth")

	resp, err := sess.DoWithAuth(sess.Client, outReq, nsid)
	if err != nil {
		http.Error(w, fmt.Sprintf("forward request: %s", err), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		slog.ErrorContext(r.Context(), "proxy: copy response body", "err", err)
	}
}

// basicAuthMiddleware requires an HTTP basic auth password matching secret
// on every request (the username is ignored). It's used to protect sap's
// internal routes when a secret is configured.
func basicAuthMiddleware(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, password, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(password), []byte(secret)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="sap-internal"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) handleClientMetadata(w http.ResponseWriter, r *http.Request) {
	metadata := s.oauthClient.Config.ClientMetadata()
	jwks := s.oauthClient.Config.PublicJWKS()
	metadata.JWKS = &jwks
	if s.clientMetadata.Name != "" {
		metadata.ClientName = &s.clientMetadata.Name
	}
	if s.clientMetadata.URI != "" {
		metadata.ClientURI = &s.clientMetadata.URI
	}
	httpx.WriteJSON(r.Context(), w, metadata)
}
