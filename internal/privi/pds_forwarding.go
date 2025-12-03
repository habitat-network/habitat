package privi

import (
	"fmt"
	"io"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/eagraf/habitat-new/internal/oauthserver"
	"github.com/eagraf/habitat-new/internal/utils"
	"github.com/rs/zerolog/log"
)

type PDSForwarding struct {
	oauthServer *oauthserver.OAuthServer
	dir         identity.Directory
}

var _ http.Handler = (*PDSForwarding)(nil)

func NewPDSForwarding(oauthServer *oauthserver.OAuthServer) *PDSForwarding {
	return &PDSForwarding{
		oauthServer: oauthServer,
		dir:         identity.DefaultDirectory(),
	}
}

// ServeHTTP implements http.Handler.
func (p *PDSForwarding) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestUri := r.URL.RequestURI()
	p.forwardRequest(w, r, requestUri)
}

func (p *PDSForwarding) forwardRequest(w http.ResponseWriter, r *http.Request, requestUri string) {
	did, dpopClient, ok := p.oauthServer.Validate(w, r)
	if !ok {
		return
	}
	// Try handling both handles and dids
	atid, err := syntax.ParseAtIdentifier(did)
	if err != nil {
		utils.LogAndHTTPError(w, err, "failed to parse at identifier", http.StatusBadRequest)
		return
	}
	id, err := p.dir.Lookup(r.Context(), *atid)
	if err != nil {
		utils.LogAndHTTPError(w, err, "failed to lookup identity", http.StatusBadRequest)
		return
	}
	pdsUrl, ok := id.Services["atproto_pds"]
	if !ok {
		utils.LogAndHTTPError(
			w,
			fmt.Errorf("identity has no pds url"),
			"identity has no pds url",
			http.StatusBadRequest,
		)
		return
	}
	// Create a new request for forwarding
	targetURL := pdsUrl.URL + requestUri
	// Only pass body for methods that support it
	var body io.Reader
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		body = r.Body
	}
	req, err := http.NewRequest(r.Method, targetURL, body)
	if err != nil {
		log.Error().Err(err).Msg("failed to create forwarding request")
		http.Error(w, "failed to create forwarding request", http.StatusInternalServerError)
		return
	}
	// Copy headers from original request
	req.Header = r.Header.Clone()
	// Forward the request using the dpopClient
	resp, err := dpopClient.Do(req)
	if err != nil {
		log.Error().Err(err).Msg("failed to forward request")
		http.Error(w, "failed to forward request", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Set(key, value)
		}
	}
	// Set the status code
	w.WriteHeader(resp.StatusCode)
	// Copy response body
	if _, err := io.Copy(w, resp.Body); err != nil {
		// Log error but can't change status code at this point
		log.Error().Err(err).Msg("failed to copy response body")
		return
	}
}
