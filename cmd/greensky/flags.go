package main

import "github.com/urfave/cli/v3"

var (
	fPort        = "port"
	fDB          = "db"
	fDomain      = "domain"
	fPearHost    = "pear-host"
	fSecret      = "secret"
	fLogLevel    = "log-level"
	fInsecureTLS = "insecure-tls"
)

func getFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    fPort,
			Usage:   "HTTP server port",
			Value:   "2600",
			Sources: cli.EnvVars("GREENSKY_PORT"),
		},
		&cli.StringFlag{
			Name:    fDB,
			Usage:   "Database connection string (sqlite:// only)",
			Value:   "sqlite://greensky.db",
			Sources: cli.EnvVars("GREENSKY_DB"),
		},
		&cli.StringFlag{
			Name:    fDomain,
			Usage:   "Publicly-accessible domain of this greensky server; also its did:web host.",
			Value:   "greensky-server.local.habitat.network",
			Sources: cli.EnvVars("GREENSKY_DOMAIN"),
		},
		&cli.StringFlag{
			Name:    fPearHost,
			Usage:   "Base URL of the pear instance, e.g. https://pear.local.habitat.network",
			Value:   "https://pear.local.habitat.network",
			Sources: cli.EnvVars("GREENSKY_PEAR_HOST"),
		},
		&cli.StringFlag{
			Name:     fSecret,
			Usage:    "Secret used in sap's OAuth client-metadata signing",
			Required: true,
			Sources:  cli.EnvVars("GREENSKY_SECRET"),
		},
		&cli.StringFlag{
			Name:    fLogLevel,
			Usage:   "Log level (debug, info, warn, error)",
			Value:   "info",
			Sources: cli.EnvVars("GREENSKY_LOG_LEVEL"),
		},
		&cli.BoolFlag{
			Name:    fInsecureTLS,
			Usage:   "Skip TLS verification when resolving did:web documents. Local dev only, for Caddy's self-signed certs.",
			Value:   false,
			Sources: cli.EnvVars("GREENSKY_INSECURE_TLS"),
		},
	}
}
