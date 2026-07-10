package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/sap"
)

// habitatDIDHeader names the DID the caller wants the proxied request to be
// authenticated as. sap looks up the OAuth session it tracks for this DID.
const habitatDIDHeader = "Habitat-Did"

// hopByHopHeaders are connection-scoped and must not be forwarded to pear per
// the HTTP/1.1 spec (RFC 7230 §6.1).
var hopByHopHeaders = []string{
	"Connection", "Transfer-Encoding", "Te", "Upgrade", "Keep-Alive",
}

type server struct {
	sap         *sap.Sap
	oauthClient *oauthclient.App
}

func NewSapServer(
	sapInstance *sap.Sap,
	oauthClient *oauthclient.App,
) *server {
	return &server{
		sap:         sapInstance,
		oauthClient: oauthClient,
	}
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(r.Context(), w, map[string]string{"status": "ok"})
}

func (s *server) handleAddOrg(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Handle string `json:"handle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
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

func (s *server) handleListOrgs(w http.ResponseWriter, r *http.Request) {
	orgs, err := s.sap.ListManagedOrgs(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if orgs == nil {
		orgs = []syntax.DID{}
	}
	httpx.WriteJSON(r.Context(), w, map[string]any{"orgs": orgs})
}

// handleAddUserSession starts a user login OAuth flow. Like the org-admin
// bootstrap it ends in a managed session sap crawls; unlike it, the caller
// (e.g. the docs server) supplies a URL to redirect the browser back to once
// sap has stored the session, so it can establish its own server session. sap
// returns the PDS authorization URL.
func (s *server) handleAddUserSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Handle   string `json:"handle"`
		Redirect string `json:"redirect"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Handle == "" || req.Redirect == "" {
		http.Error(w, "handle and redirect are required", http.StatusBadRequest)
		return
	}

	redirectURL, err := s.sap.StartUserLogin(r.Context(), req.Handle, req.Redirect)
	if err != nil {
		http.Error(w, fmt.Sprintf("start user login: %s", err), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(r.Context(), w, map[string]string{"redirect_url": redirectURL})
}

// handleGetLogin resolves a login token (handed to the redirect target when a
// user login completes) to the DID that authenticated, so the docs server can
// bind its server session to the user.
func (s *server) handleGetLogin(w http.ResponseWriter, r *http.Request) {
	loginToken := r.URL.Query().Get("login")
	if loginToken == "" {
		http.Error(w, "missing login query param", http.StatusBadRequest)
		return
	}
	did, err := s.sap.GetCompletedLogin(r.Context(), loginToken)
	if err != nil {
		http.Error(w, fmt.Sprintf("unknown login token: %s", err), http.StatusNotFound)
		return
	}
	httpx.WriteJSON(r.Context(), w, map[string]string{"did": did.String()})
}

func (s *server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	redirect, err := s.sap.CompleteLogin(r.Context(), r.URL.Query())
	if err != nil {
		http.Error(w, fmt.Sprintf("process callback: %s", err), http.StatusInternalServerError)
		return
	}

	// A user login redirects the browser back to the service that started it
	// (with a login token); an org login has no redirect and just reports success.
	if redirect != "" {
		w.Header().Set("Location", redirect)
		w.WriteHeader(http.StatusSeeOther)
		return
	}

	slog.InfoContext(r.Context(), "org oauth complete")
	w.WriteHeader(http.StatusOK)
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

	client, err := s.sap.GetClient(r.Context(), did)
	if err != nil {
		http.Error(w, fmt.Sprintf("no tracked session for %s: %s", did, err), http.StatusBadGateway)
		return
	}

	// The OAuth client's transport resolves this path-only URL against the org's
	// Habitat host and attaches the access token.
	nsid := strings.TrimPrefix(r.URL.Path, "/proxy/")
	target := url.URL{Path: "/xrpc/" + nsid, RawQuery: r.URL.RawQuery}

	var body io.Reader
	if r.Body != nil {
		body = r.Body
	}
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), body)
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
	outReq.Header.Del("Authorization")

	resp, err := client.Do(outReq)
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
