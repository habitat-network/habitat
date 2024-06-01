package server

import (
	"crypto/tls"
	"net"
	"net/http"

	"github.com/rs/zerolog/log"
)

// config provided to http.Server.ListenAndServeTLS()
type TLSConfig struct {
	config   *tls.Config
	certFile string
	keyFile  string
}

// ServerOptions provide optional config for an http.Server passed to serveFn()
type ServerOption struct {
	listener  net.Listener
	tlsConfig *TLSConfig
}

// conventional way to supply arbitrary and optional arguments as options to a function.
type Option func(*ServerOption)

// WithTLSConfig provides serverOption with tlsConfig
func WithTLSConfig(config *tls.Config, certFilePath string, keyFilePath string) Option {
	return func(so *ServerOption) {
		if config != nil {
			so.tlsConfig = &TLSConfig{
				config:   config,
				certFile: certFilePath,
				keyFile:  keyFilePath,
			}
		}
	}
}

func WithListener(listener net.Listener) Option {
	return func(so *ServerOption) {
		so.listener = listener
	}
}

// serveFn takes in an http.Server and additional config and returns a callback that can be run in a separate go-routine.
func ServeFn(srv *http.Server, name string, opts ...Option) func() error {
	options := &ServerOption{}
	for _, o := range opts {
		o(options)
	}
	return func() error {
		if options.listener == nil {
			ln, err := net.Listen("tcp", srv.Addr)
			if err != nil {
				return err
			}
			options.listener = ln
		}
		defer options.listener.Close()

		if options.tlsConfig != nil && options.tlsConfig.config != nil {
			srv.TLSConfig = options.tlsConfig.config
		}

		var err error
		if srv.TLSConfig != nil && options.tlsConfig != nil {
			log.Info().Msgf("Starting Habitat server[%s] at %s over TLS", name, srv.Addr)
			err = srv.ServeTLS(options.listener, options.tlsConfig.certFile, options.tlsConfig.keyFile)
		} else {
			log.Warn().Msgf("No TLS config found: starting Habitat server[%s] at %s without TLS enabled", name, srv.Addr)
			err = srv.Serve(options.listener)
		}
		if err != http.ErrServerClosed {
			log.Err(err).Msgf("Habitat server[%s] closed abnormally", name)
			return err
		}
		return nil
	}
}
