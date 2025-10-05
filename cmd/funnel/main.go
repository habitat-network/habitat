package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/eagraf/habitat-new/internal/node/logging"
	"tailscale.com/tsnet"
)

func main() {
	// read command line args
	port := os.Args[1]
	hostName := os.Args[2]

	logger := logging.NewLogger()

	nodeConfig, err := config.NewNodeConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("error loading node config")
	}

	server := tsnet.Server{
		Hostname: hostName,
		AuthKey:  nodeConfig.TailscaleAuthkey(),
	}
	defer server.Close()

	ln, err := server.ListenFunnel("tcp", ":443")
	if err != nil {
		logger.Fatal().Err(err).Msg("unable to listen")
	}

	localUrl, err := url.Parse("http://localhost:" + port)
	if err != nil {
		logger.Fatal().Err(err).Msg("unable to parse url")
	}
	proxy := httputil.NewSingleHostReverseProxy(localUrl)

	err = http.Serve(ln, proxy)
	if err != nil {
		logger.Fatal().Err(err).Msg("unable to serve")
	}
}
