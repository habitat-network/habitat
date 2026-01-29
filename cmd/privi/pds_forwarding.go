package main

import (
	"io"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/rs/zerolog/log"
)

type pdsForwarding struct {
	oauthServer *oauthserver.OAuthServer
	credStore   pdscred.PDSCredentialStore
	oauthClient oauthclient.OAuthClient
	dir         identity.Directory
}

var _ http.Handler = (*pdsForwarding)(nil)

func newPDSForwarding(
	credStore pdscred.PDSCredentialStore,
	oauthServer *oauthserver.OAuthServer,
	oauthClient oauthclient.OAuthClient,
) *pdsForwarding {
	return &pdsForwarding{
		oauthServer: oauthServer,
		credStore:   credStore,
		oauthClient: oauthClient,
		dir:         identity.DefaultDirectory(),
	}
}

// ServeHTTP implements http.Handler.
func (p *pdsForwarding) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	did, ok := p.oauthServer.Validate(w, r)
	if !ok {
		return
	}
	id, err := p.dir.LookupDID(r.Context(), did)
	if err != nil {
		utils.LogAndHTTPError(w, err, "failed to lookup identity", http.StatusBadRequest)
		return
	}
	dpopClient := oauthclient.NewAuthedDpopHttpClient(
		id,
		p.credStore,
		p.oauthClient,
		&oauthclient.MemoryNonceProvider{},
	)
	// Only pass body for methods that support it
	var body io.Reader
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		body = r.Body
	}
	req, err := http.NewRequest(r.Method, r.URL.RequestURI(), body)
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
