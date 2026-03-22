package main

import (
	"io"
	"net/http"

	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/rs/zerolog/log"
)

type pdsForwarding struct {
	oauthServer      *oauthserver.OAuthServer
	pdsClientFactory pdsclient.HttpClientFactory
}

var _ http.Handler = (*pdsForwarding)(nil)

func newPDSForwarding(
	credStore pdscred.PDSCredentialStore,
	oauthServer *oauthserver.OAuthServer,
	pdsClientFactory pdsclient.HttpClientFactory,
) *pdsForwarding {
	return &pdsForwarding{
		oauthServer:      oauthServer,
		pdsClientFactory: pdsClientFactory,
	}
}

// ServeHTTP implements http.Handler.
func (p *pdsForwarding) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	did, ok := p.oauthServer.Validate(w, r)
	if !ok {
		return
	}
	// Only pass body for methods that support it
	var body io.Reader
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		body = r.Body
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.RequestURI(), body)
	if err != nil {
		utils.LogAndHTTPError(w, err, "[pds forwarding]: failed to create forwarding request", http.StatusInternalServerError)
		return
	}
	// Copy headers from original request
	req.Header = r.Header.Clone()

	dpopClient, err := p.pdsClientFactory.NewClient(r.Context(), did)
	if err != nil {
		utils.LogAndHTTPError(w, err, "[pds forwarding]: failed to create dpop client", http.StatusInternalServerError)

		return
	}

	// Forward the request using the dpopClient
	resp, err := dpopClient.Do(req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "[pds forwarding]: failed to forward request", http.StatusUnauthorized)
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
		if utils.ShouldLog(err) {
			log.Error().Err(err).Msg("[pds forwarding]: failed to copy response body")
		}
		return
	}
}
