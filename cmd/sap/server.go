package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
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
	sap         *sap.Sap
	oauthClient *oauth.ClientApp

	mu              sync.Mutex
	pendingReturnTo map[string]string // DID string -> return_to URL
}

func NewSapServer(
	sapInstance *sap.Sap,
	oauthClient *oauth.ClientApp,
) *server {
	return &server{
		sap:             sapInstance,
		oauthClient:     oauthClient,
		pendingReturnTo: make(map[string]string),
	}
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(r.Context(), w, map[string]string{"status": "ok"})
}

func (s *server) handleAddOrg(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Handle   string `json:"handle"`
		ReturnTo string `json:"return_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// oauth.ClientApp.StartAuthFlow has no hook to carry caller state through
	// the OAuth state param and doesn't return the resolved DID, so when the
	// caller wants to be redirected back we resolve the identifier to a DID
	// ourselves (the same lookup StartAuthFlow performs internally) and stash
	// return_to keyed by that DID for handleOAuthCallback to pick up later.
	// Resolution failures here must not block the StartAuthFlow call below —
	// they just mean the caller won't get redirected back.
	if req.ReturnTo != "" {
		if atid, err := syntax.ParseAtIdentifier(req.Handle); err == nil {
			if ident, err := s.oauthClient.Dir.Lookup(r.Context(), atid); err == nil {
				s.mu.Lock()
				s.pendingReturnTo[ident.DID.String()] = req.ReturnTo
				s.mu.Unlock()
			}
		}
	}

	redirectURL, err := s.oauthClient.StartAuthFlow(r.Context(), req.Handle)
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

// orgSession is the wire shape for each entry in handleListOrgs's response:
// the tracked DID paired with the session ID sap resumes it with, so callers
// (e.g. chalk) can drive handleProxy's Habitat-Session header without
// guessing.
type orgSession struct {
	DID       syntax.DID `json:"did"`
	SessionID string     `json:"sessionId"`
}

func (s *server) handleListOrgs(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.sap.SessionList(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	orgs := make([]orgSession, len(sessions))
	for i, sess := range sessions {
		orgs[i] = orgSession{DID: sess.DID, SessionID: sess.SessionID}
	}
	httpx.WriteJSON(r.Context(), w, map[string]any{"orgs": orgs})
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
	sessionID := r.Header.Get(habitatSessionHeader)
	if sessionID == "" {
		http.Error(w, "missing "+habitatSessionHeader+" header", http.StatusBadRequest)
		return
	}
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

	var body io.Reader
	if r.Body != nil {
		body = r.Body
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

func (s *server) handleClientMetadata(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(r.Context(), w, s.oauthClient.Config.ClientMetadata())
}
