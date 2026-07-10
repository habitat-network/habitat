package forwarding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/pdsclient"
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
type serviceProxy struct {
	oauth            authn.Method
	hive             hive.Hive
	dir              identity.Directory
	httpClient       *http.Client
	pdsClientFactory pdsclient.HttpClientFactory
}

// NewServiceProxy constructs a ServiceProxy, which is a MiddlewareFunc and intercepts requests that have atproto-proxy in the headers.
// oauth validates the incoming caller's session.
// hive signs service auth JWTs for forwarded requests via hive.SignServiceAuth.
// dir resolves external DIDs to find service endpoints.
func NewServiceProxy(
	oauth authn.Method,
	hive hive.Hive,
	dir identity.Directory,
	clientFactory pdsclient.HttpClientFactory,
) func(http.Handler) http.Handler /* type of mux.MiddlewareFunc */ {
	sp := &serviceProxy{
		oauth:            oauth,
		hive:             hive,
		dir:              dir,
		httpClient:       &http.Client{},
		pdsClientFactory: clientFactory,
	}

	// Requests carrying an Atproto-Proxy header on an XRPC path are intercepted and
	// forwarded to the specified service; all other requests pass through unchanged.
	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proxyHeader := r.Header.Get("Atproto-Proxy")
			if proxyHeader == "" || !strings.HasPrefix(r.URL.Path, "/xrpc/") {
				next.ServeHTTP(w, r)
				return
			}
			sp.proxy(w, r, proxyHeader)
		})
	}
	return middleware
}

func (s *serviceProxy) proxy(w http.ResponseWriter, r *http.Request, proxyHeader string) {
	ctx := r.Context()
	// Validate the caller's OAuth session before acting on their behalf.
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.oauth),
		authn.WithSupportedCredentials(authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	// Parse "did#serviceId" — the "#" separator is required by the AT Protocol spec.
	rawDID, serviceID, ok := strings.Cut(proxyHeader, "#")
	if !ok {
		httpx.WriteInvalidRequest(ctx, w, "malformed Atproto-Proxy header: missing #", nil)
		return
	}

	targetDID, ok := httpx.ParseDIDInput(ctx, w, rawDID, "service did")
	if !ok {
		return
	}

	// Use context.Background() to avoid cached context cancelled errors:
	// https://github.com/bluesky-social/indigo/pull/1345
	id, err := s.dir.LookupDID(context.Background(), targetDID)
	if err != nil {
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"[service proxy]: failed to resolve proxy target DID",
			http.StatusBadGateway,
		)
		return
	}

	svc, ok := id.Services[serviceID]
	if !ok {
		utils.WriteHTTPError(
			w,
			fmt.Errorf("service %q not found in DID document for %s", serviceID, targetDID),
			http.StatusBadRequest,
		)
		return
	}

	nsid, ok := httpx.ParseNSIDInput(ctx, w, strings.TrimPrefix(r.URL.Path, "/xrpc/"), "nsid")
	if !ok {
		return
	}

	// Habitat owns user signing keys, so it signs service auth tokens on behalf
	// of users — the same role a PDS fills when calling com.atproto.server.getServiceAuth.
	token, err := s.hive.SignJWT(
		ctx,
		credInfo.Subject,
		map[string]any{},
		jwt.MapClaims{
			"exp": jwt.NewNumericDate(time.Now().Add(time.Minute)),
			"iat": jwt.NewNumericDate(time.Now()),
			"iss": credInfo.Subject,
			"aud": targetDID,
			"jti": utils.RandomNonce(16),
			"lxm": nsid,
		},
	)
	if errors.Is(err, identity.ErrDIDNotFound) {
		token, err = s.fetchRemoteServiceAuth(ctx, credInfo, proxyHeader, nsid)
		if err != nil {
			httpx.WriteServerError(
				ctx,
				w,
				fmt.Errorf("failed to fetch remote service auth: %w", err),
			)
			return
		}
	} else if err != nil {
		httpx.WriteServerError(
			ctx,
			w,
			fmt.Errorf("failed to sign service auth: %w", err),
		)
		return
	}

	base, err := url.Parse(svc.URL)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid service URL in aud DID document", err)
		return
	}
	requestURI, err := url.Parse(r.URL.RequestURI())
	if err != nil {
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"[service proxy]: failed to parse request URI",
			http.StatusInternalServerError,
		)
		return
	}
	forwardURL := base.ResolveReference(requestURI)

	var body io.Reader
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		body = r.Body
	}

	outReq, err := http.NewRequestWithContext(ctx, r.Method, forwardURL.String(), body)
	if err != nil {
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"[service proxy]: failed to build forwarded request",
			http.StatusInternalServerError,
		)
		return
	}

	// Clone headers, then scrub hop-by-hop headers and any headers named in the
	// Connection value (per RFC 7230 §6.1), then override auth.
	outReq.Header = r.Header.Clone()
	for _, h := range strings.Split(outReq.Header.Get("Connection"), ",") {
		outReq.Header.Del(strings.TrimSpace(h))
	}
	for _, h := range hopByHopHeaders {
		outReq.Header.Del(h)
	}
	outReq.Header.Set("Authorization", "Bearer "+token)
	// DPoP proofs are bound to Habitat's endpoint and must not be forwarded.
	outReq.Header.Del("DPoP")
	// Strip Atproto-Proxy to prevent the target from attempting further proxying.
	outReq.Header.Del("Atproto-Proxy")

	resp, err := s.httpClient.Do(outReq)
	if err != nil {
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"[service proxy]: forwarded request failed",
			http.StatusBadGateway,
		)
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
		if utils.ShouldLog(err) {
			slog.ErrorContext(
				ctx,
				"[service proxy]: failed to copy response body",
				"err",
				err,
			)
		}
	}
}

func (s *serviceProxy) fetchRemoteServiceAuth(
	ctx context.Context,
	credInfo *authn.CredentialInfo,
	proxyHeader string, nsid syntax.NSID,
) (string, error) {
	slog.WarnContext(ctx, "fetching remove service auth", "proxy", proxyHeader, "nsid", nsid)
	pdsClient, err := s.pdsClientFactory.NewClient(ctx, credInfo.Subject)
	if err != nil {
		return "", fmt.Errorf("failed to create PDS client: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"/xrpc/com.atproto.server.getServiceAuth?"+url.Values{
			"aud": {proxyHeader},
			"lxm": {nsid.String()},
		}.Encode(),
		nil,
	)
	slog.WarnContext(ctx, "fetching remove service auth", "req", req.URL.String())
	if err != nil {
		return "", fmt.Errorf("failed to build service auth request: %w", err)
	}
	resp, err := pdsClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get service auth: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var serviceAuth struct {
		Token   string
		Error   string
		Message string
	}
	if err := json.NewDecoder(resp.Body).Decode(&serviceAuth); err != nil {
		return "", fmt.Errorf("failed to decode service auth: %w", err)
	}
	if serviceAuth.Token == "" {
		return "", fmt.Errorf(
			"failed to fetch remove service auth: %s %s",
			serviceAuth.Error,
			serviceAuth.Message,
		)
	}
	slog.WarnContext(ctx, "fetched remove service auth", "token", serviceAuth.Token)
	return serviceAuth.Token, nil
}
