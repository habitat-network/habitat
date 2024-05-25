package api

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/reverse_proxy"
	"github.com/rs/zerolog"
)

const CertificateDir = "/dev_certificates"

func NewAPIServer(
	router http.Handler,
	logger *zerolog.Logger,
	proxyRules reverse_proxy.RuleSet,
	nodeConfig *config.NodeConfig,
) (*http.Server, error) {
	srv := &http.Server{Addr: fmt.Sprintf(":%s", constants.DefaultPortHabitatAPI), Handler: router}
	tlsConfig, err := nodeConfig.TLSConfig()
	if err != nil {
		return nil, err
	}
	srv.TLSConfig = tlsConfig

	// Start the server
	url, err := url.Parse("http://localhost:3000")
	if err != nil {
		return nil, fmt.Errorf("error parsing URL: %s", err)
	}
	err = proxyRules.Add("Habitat API", &reverse_proxy.RedirectRule{
		ForwardLocation: url,
		Matcher:         "/habitat/api",
	})
	if err != nil {
		return nil, fmt.Errorf("error adding proxy rule: %s", err)
	}

	logger.Info().Msgf("Starting Habitat API server at %s", srv.Addr)
	return srv, nil
}
