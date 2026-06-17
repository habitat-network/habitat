package forwarding

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/utils"
)

// hopByHopHeaders are stripped before forwarding per the HTTP/1.1 spec.
var hopByHopHeaders = []string{
	"Connection", "Transfer-Encoding", "Te", "Upgrade", "Keep-Alive",
}

// ServiceProxy implements AT Protocol service proxying: when an incoming XRPC
// request carries an Atproto-Proxy header, it validates the caller's OAuth
// session and forwards the request to the specified service using a service
// auth JWT signed on the caller's behalf.
type ServiceProxy struct {
	oauth authn.Method
	h     hive.Hive
	dir   identity.Directory
}

// NewServiceProxy constructs a ServiceProxy.
// oauth validates the incoming caller's session.
// h signs service auth JWTs for forwarded requests via hive.SignServiceAuth.
// dir resolves external DIDs to find service endpoints.
func NewServiceProxy(oauth authn.Method, h hive.Hive, dir identity.Directory) *ServiceProxy {
	return &ServiceProxy{
		oauth: oauth,
		h:     h,
		dir:   dir,
	}
}

// Middleware returns a gorilla/mux-compatible middleware handler. Requests
// carrying an Atproto-Proxy header on an XRPC path are intercepted and
// forwarded to the specified service; all other requests pass through unchanged.
func (s *ServiceProxy) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHeader := r.Header.Get("Atproto-Proxy")
		if proxyHeader == "" || !strings.HasPrefix(r.URL.Path, "/xrpc/") {
			next.ServeHTTP(w, r)
			return
		}
		s.proxy(w, r, proxyHeader)
	})
}

func (s *ServiceProxy) proxy(w http.ResponseWriter, r *http.Request, proxyHeader string) {
	// Parse "did#serviceId" — the "#" separator is required by the AT Protocol spec.
	rawDID, serviceID, ok := strings.Cut(proxyHeader, "#")
	if !ok {
		utils.WriteHTTPError(w, fmt.Errorf("malformed Atproto-Proxy header: missing '#'"), http.StatusBadRequest)
		return
	}

	// Validate the caller's OAuth session before acting on their behalf.
	callerDID, ok := authn.Validate(w, r, s.oauth)
	if !ok {
		return
	}

	targetDID, err := syntax.ParseDID(rawDID)
	if err != nil {
		utils.WriteHTTPError(w, fmt.Errorf("invalid DID in Atproto-Proxy header: %w", err), http.StatusBadRequest)
		return
	}

	// Use context.Background() to avoid cached context cancelled errors:
	// https://github.com/bluesky-social/indigo/pull/1345
	id, err := s.dir.LookupDID(context.Background(), targetDID)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "[service proxy]: failed to resolve proxy target DID", http.StatusBadGateway)
		return
	}

	svc, ok := id.Services[serviceID]
	if !ok {
		utils.WriteHTTPError(w, fmt.Errorf("service %q not found in DID document for %s", serviceID, targetDID), http.StatusBadRequest)
		return
	}

	nsidStr := strings.TrimPrefix(r.URL.Path, "/xrpc/")
	nsid, err := syntax.ParseNSID(nsidStr)
	if err != nil {
		utils.WriteHTTPError(w, fmt.Errorf("invalid NSID in path: %w", err), http.StatusBadRequest)
		return
	}

	// Habitat owns user signing keys, so it signs service auth tokens on behalf
	// of users — the same role a PDS fills when calling com.atproto.server.getServiceAuth.
	jwt, err := s.h.SignServiceAuth(r.Context(), callerDID, targetDID.String(), 60*time.Second, &nsid)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "[service proxy]: failed to sign service auth", http.StatusInternalServerError)
		return
	}

	base, err := url.Parse(svc.URL)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "[service proxy]: invalid service URL in DID document", http.StatusBadGateway)
		return
	}
	requestURI, err := url.Parse(r.URL.RequestURI())
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "[service proxy]: failed to parse request URI", http.StatusInternalServerError)
		return
	}
	forwardURL := base.ResolveReference(requestURI)

	var body io.Reader
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		body = r.Body
	}

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, forwardURL.String(), body)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "[service proxy]: failed to build forwarded request", http.StatusInternalServerError)
		return
	}

	// Clone headers, then scrub hop-by-hop and override auth.
	outReq.Header = r.Header.Clone()
	for _, h := range hopByHopHeaders {
		outReq.Header.Del(h)
	}
	outReq.Header.Set("Authorization", "Bearer "+jwt)
	// Strip Atproto-Proxy to prevent the target from attempting further proxying.
	outReq.Header.Del("Atproto-Proxy")

	resp, err := http.DefaultClient.Do(outReq)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "[service proxy]: forwarded request failed", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Set(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		if utils.ShouldLog(err) {
			slog.ErrorContext(r.Context(), "[service proxy]: failed to copy response body", "err", err)
		}
	}
}
