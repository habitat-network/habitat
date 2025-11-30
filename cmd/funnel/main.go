package main

import (
	"context"
	"flag"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"tailscale.com/tsnet"
)

func main() {
	// read command line args
	port := os.Args[1]
	hostName := os.Args[2]

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	flag.BoolFunc("debug", "", func(_ string) error {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		return nil
	})
	flag.Parse()

	server := &tsnet.Server{
		Hostname: hostName,
		Logf: func(format string, args ...any) {
			log.Debug().Msgf(format, args...)
		},
		Dir: ".habitat/funnel/" + hostName,
	}

	ln, err := server.ListenFunnel("tcp", ":443")
	if err != nil {
		log.Fatal().Err(err).Msg("unable to listen")
	}

	localUrl, err := url.Parse("http://localhost:" + port)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to parse url")
	}
	proxy := httputil.NewSingleHostReverseProxy(localUrl)

	// Create HTTP server
	httpServer := &http.Server{
		Handler: proxy,
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Run server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		log.Info().Msg("starting funnel server")
		serverErr <- httpServer.Serve(ln)
	}()

	// Wait for interrupt signal or server error
	select {
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	case sig := <-sigChan:
		log.Info().Msgf("received signal %v, shutting down gracefully", sig)
	}

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Warn().Err(err).Msg("error during HTTP server shutdown")
	}
	// Close the tsnet server
	if err := server.Close(); err != nil {
		log.Warn().Err(err).Msg("error closing tsnet server")
	}
	log.Info().Msg("server stopped - exiting")
}
