package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/sap"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// habitatDIDHeader and habitatSessionHeader name the OAuth session the caller
// wants the proxied request authenticated as; both are provided by the caller.
const (
	habitatDIDHeader     = "Habitat-Did"
	habitatSessionHeader = "Habitat-Session-Id"
)

// hopByHopHeaders are connection-scoped and must not be forwarded to pear per
// the HTTP/1.1 spec (RFC 7230 §6.1).
var hopByHopHeaders = []string{
	"Connection", "Transfer-Encoding", "Te", "Upgrade", "Keep-Alive",
}

type server struct {
	sap         *sap.Sap
	oauthClient *oauth.ClientApp
	// serviceAuth validates the service-auth JWTs on inbound notifyWrite /
	// notifySpaceDeleted calls from space hosts.
	serviceAuth *auth.ServiceAuthValidator
}

func NewSapServer(
	sapInstance *sap.Sap,
	oauthClient *oauth.ClientApp,
	serviceAuth *auth.ServiceAuthValidator,
) *server {
	return &server{
		sap:         sapInstance,
		oauthClient: oauthClient,
		serviceAuth: serviceAuth,
	}
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(r.Context(), w, map[string]string{"status": "ok"})
}

func (s *server) handleAddSession(w http.ResponseWriter, r *http.Request) {
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

func (s *server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.sap.Sessions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if sessions == nil {
		sessions = []syntax.DID{}
	}
	httpx.WriteJSON(r.Context(), w, map[string]any{"sessions": sessions})
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
		http.Error(w, fmt.Sprintf("save session: %s", err), http.StatusInternalServerError)
		return
	}

	slog.InfoContext(r.Context(), "session oauth complete", "did", sessionData.AccountDID)
	w.WriteHeader(http.StatusOK)
}

// handleProxy forwards an XRPC request to pear, authenticated as the OAuth
// session the caller names with the Habitat-Did and Habitat-Session-Id
// headers. The path after /proxy/ is the XRPC method, forwarded to pear as
// /xrpc/<method> with the original method, query params, headers, and body
// preserved.
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
			fmt.Sprintf("no session %s for %s: %s", sessionID, did, err),
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
	// Resolve the target against the Habitat host tracked in the OAuth
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

// authorizeNotify validates the request's service-auth token for the given
// lexicon method and checks that its issuer is the space's authority (the
// space owner, who signs notify deliveries). On failure it writes the HTTP
// error and returns false.
func (s *server) authorizeNotify(
	w http.ResponseWriter,
	r *http.Request,
	lxm syntax.NSID,
	space habitat_syntax.SpaceURI,
) bool {
	ctx := r.Context()
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		httpx.WriteInvalidRequest(ctx, w, "missing bearer token",
			fmt.Errorf("no service auth"))
		return false
	}
	issuer, err := s.serviceAuth.Validate(ctx, token, &lxm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return false
	}
	if issuer != space.SpaceOwner() {
		httpx.WriteInvalidRequest(ctx, w, "not the space authority",
			fmt.Errorf("issuer %q is not the authority for %q", issuer, space))
		return false
	}
	return true
}

func (s *server) handleNotifyWrite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceNotifyWriteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode notifyWrite", err)
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space")
	if !ok {
		return
	}
	repo, ok := httpx.ParseDIDInput(ctx, w, input.Repo, "repo")
	if !ok {
		return
	}
	rev, err := syntax.ParseTID(input.Rev)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "parse rev", err)
		return
	}
	hash, err := decodeBytesField(input.Hash)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode hash", err)
		return
	}
	if !s.authorizeNotify(w, r, "network.habitat.space.notifyWrite", space) {
		return
	}

	if err := s.sap.NotifyWrite(ctx, space, repo, rev, hash); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("notify write: %w", err))
		return
	}
	slog.InfoContext(ctx, "notifyWrite queued sync",
		"space", space, "repo", repo, "rev", input.Rev)
	w.WriteHeader(http.StatusOK)
}

func (s *server) handleNotifySpaceDeleted(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceNotifySpaceDeletedInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode notifySpaceDeleted", err)
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space")
	if !ok {
		return
	}
	if !s.authorizeNotify(w, r, "network.habitat.space.notifySpaceDeleted", space) {
		return
	}
	if err := s.sap.NotifySpaceDeleted(ctx, space); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("notify space deleted: %w", err))
		return
	}
	slog.InfoContext(ctx, "notifySpaceDeleted dropped tracking", "space", space)
	w.WriteHeader(http.StatusOK)
}

// decodeBytesField decodes a lexicon bytes field, which JSON-decodes into a
// base64 (std) string, into raw bytes. Absent fields decode to nil.
func decodeBytesField(v any) ([]byte, error) {
	switch s := v.(type) {
	case nil:
		return nil, nil
	case string:
		if s == "" {
			return nil, nil
		}
		return base64.StdEncoding.DecodeString(s)
	default:
		return nil, fmt.Errorf("expected base64 string, got %T", v)
	}
}
