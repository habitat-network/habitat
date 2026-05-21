package forwarding

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/rs/zerolog/log"
)

// targetRoutedMethods maps XRPC method names that should be forwarded to the
// target's PDS (rather than the caller's) to the query param that identifies
// the target. These are all public, unauthenticated endpoints.
var targetRoutedMethods = map[string]string{
	"com.atproto.repo.getRecord":       "repo",
	"com.atproto.repo.listRecords":     "repo",
	"com.atproto.repo.describeRepo":    "repo",
	"com.atproto.sync.getRepo":         "did",
	"com.atproto.sync.getRecord":       "did",
	"com.atproto.sync.listBlobs":       "did",
	"com.atproto.sync.getLatestCommit": "did",
	"com.atproto.sync.getRepoStatus":   "did",
	"com.atproto.sync.getBlob":         "did",
}

type PDSForwarding struct {
	oauth           authn.Method
	clientApp       *oauth.ClientApp
	dir             identity.Directory
	plainHTTPClient *http.Client
}

var _ http.Handler = (*PDSForwarding)(nil)

func NewPDSForwarding(
	oauthServer authn.Method,
	clientApp *oauth.ClientApp,
	dir identity.Directory,
) *PDSForwarding {
	return &PDSForwarding{
		oauth:           oauthServer,
		clientApp:       clientApp,
		dir:             dir,
		plainHTTPClient: &http.Client{},
	}
}

// ServeHTTP implements http.Handler.
func (p *PDSForwarding) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	method := strings.TrimPrefix(r.URL.Path, "/xrpc/")
	if paramName, ok := targetRoutedMethods[method]; ok {
		target := r.URL.Query().Get(paramName)
		if target == "" {
			utils.LogAndHTTPError(w,
				fmt.Errorf("missing required query param: %s", paramName),
				"[pds forwarding]: missing target param",
				http.StatusBadRequest,
			)
			return
		}

		atid, err := syntax.ParseAtIdentifier(target)
		if err != nil {
			utils.LogAndHTTPError(
				w,
				err,
				"[pds forwarding]: invalid target identifier",
				http.StatusBadRequest,
			)
			return
		}

		p.serveTargetPDS(w, r, atid)
		return
	}
	p.serveCallerPDS(w, r)
}

func (p *PDSForwarding) serveTargetPDS(
	w http.ResponseWriter,
	r *http.Request,
	caller syntax.AtIdentifier,
) {
	// Use context.Background() to avoid cached context cancelled errors: https://github.com/bluesky-social/indigo/pull/1345
	id, err := p.dir.Lookup(context.Background(), caller)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"[pds forwarding]: failed to lookup target identity",
			http.StatusBadGateway,
		)
		return
	}

	pdsEndpoint, ok := id.Services["atproto_pds"]
	if !ok {
		utils.LogAndHTTPError(w,
			fmt.Errorf("no atproto_pds service for %s", id.DID),
			"[pds forwarding]: target has no PDS service",
			http.StatusBadGateway,
		)
		return
	}

	pdsURL, err := url.Parse(pdsEndpoint.URL)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"[pds forwarding]: failed to parse target PDS URL",
			http.StatusInternalServerError,
		)
		return
	}

	requestURI, err := url.Parse(r.URL.RequestURI())
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"[pds forwarding]: failed to parse request URI",
			http.StatusInternalServerError,
		)
		return
	}
	targetURL := pdsURL.ResolveReference(requestURI)

	var body io.Reader
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		body = r.Body
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL.String(), body)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"[pds forwarding]: failed to create forwarding request",
			http.StatusInternalServerError,
		)
		return
	}

	req.Header = r.Header.Clone()
	for _, h := range strings.Split(req.Header.Get("Connection"), ",") {
		req.Header.Del(strings.TrimSpace(h))
	}
	req.Header.Del("Connection")
	req.Header.Del("Upgrade")
	req.Header.Del("Keep-Alive")
	req.Header.Del("Transfer-Encoding")
	req.Header.Del("Te")
	// Strip auth headers — these are public endpoints and forwarding them to a
	// third-party PDS would be a security issue.
	req.Header.Del("Authorization")
	req.Header.Del("DPoP")

	resp, err := p.plainHTTPClient.Do(req)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"[pds forwarding]: failed to forward request to target PDS",
			http.StatusBadGateway,
		)
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
			log.Error().Err(err).Msg("[pds forwarding]: failed to copy response body")
		}
	}
}

func (p *PDSForwarding) serveCallerPDS(w http.ResponseWriter, r *http.Request) {
	did, ok := p.oauth.Validate(w, r)
	if !ok {
		return
	}

	id, err := p.dir.LookupDID(r.Context(), did)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"[pds forwarding]: failed to lookup caller identity",
			http.StatusBadGateway,
		)
		return
	}
	pdsEndpoint, ok := id.Services["atproto_pds"]
	if !ok {
		utils.LogAndHTTPError(w,
			fmt.Errorf("no atproto_pds service for %s", id.DID),
			"[pds forwarding]: caller has no PDS service",
			http.StatusBadGateway,
		)
		return
	}
	pdsURL, err := url.Parse(pdsEndpoint.URL)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"[pds forwarding]: failed to parse PDS URL",
			http.StatusInternalServerError,
		)
		return
	}
	requestURI, err := url.Parse(r.URL.RequestURI())
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"[pds forwarding]: failed to parse request URI",
			http.StatusInternalServerError,
		)
		return
	}
	targetURL := pdsURL.ResolveReference(requestURI)

	var body io.Reader
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		body = r.Body
	}

	fwdReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL.String(), body)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"[pds forwarding]: failed to create forwarding request",
			http.StatusInternalServerError,
		)
		return
	}
	fwdReq.Header = r.Header.Clone()
	for _, h := range strings.Split(fwdReq.Header.Get("Connection"), ",") {
		fwdReq.Header.Del(strings.TrimSpace(h))
	}
	fwdReq.Header.Del("Connection")
	fwdReq.Header.Del("Upgrade")
	fwdReq.Header.Del("Keep-Alive")
	fwdReq.Header.Del("Transfer-Encoding")
	fwdReq.Header.Del("Te")
	// Strip client auth headers — DoWithAuth adds its own DPoP auth
	fwdReq.Header.Del("Authorization")
	fwdReq.Header.Del("DPoP")

	sess, err := p.clientApp.ResumeSession(r.Context(), did, "default")
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"[pds forwarding]: failed to resume session",
			http.StatusInternalServerError,
		)
		return
	}

	nsidStr := strings.TrimPrefix(r.URL.Path, "/xrpc/")
	nsid, err := syntax.ParseNSID(nsidStr)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"[pds forwarding]: invalid NSID",
			http.StatusBadRequest,
		)
		return
	}

	resp, err := sess.DoWithAuth(http.DefaultClient, fwdReq, nsid)
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"[pds forwarding]: failed to forward request",
			http.StatusBadGateway,
		)
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
			log.Error().Err(err).Msg("[pds forwarding]: failed to copy response body")
		}
	}
}
