package main

import "github.com/urfave/cli/v3"

var (
	fPort     = "port"
	fDB       = "db"
	fPearHost = "pear-host"
	fDomain   = "domain"
	fSecret   = "secret"
	fLogLevel = "log-level"
)

func getFlags() []cli.Flag {
	return []cli.Flag{
		&cli.IntFlag{
			Name:    fPort,
			Usage:   "Port to run the server on",
			Value:   8091,
			Sources: cli.NewValueSourceChain(cli.EnvVar("SEARCH_PORT")),
		},
		&cli.StringFlag{
			Name:     fDB,
			Usage:    "Postgres connection string for the search index database",
			Required: true,
			Sources:  cli.NewValueSourceChain(cli.EnvVar("SEARCH_PGURL")),
		},
		&cli.StringFlag{
			Name:     fPearHost,
			Usage:    "Base URL of the pear instance to index, e.g. https://pear.example.com",
			Required: true,
			Sources:  cli.NewValueSourceChain(cli.EnvVar("SEARCH_PEAR_HOST")),
		},
		&cli.StringFlag{
			Name:    fDomain,
			Usage:   "Publicly-accessible domain of this search instance (for sap's OAuth callback)",
			Value:   "search.local.habitat.network",
			Sources: cli.NewValueSourceChain(cli.EnvVar("SEARCH_DOMAIN")),
		},
		&cli.StringFlag{
			Name:     fSecret,
			Usage:    "Secret used in sap's OAuth client-metadata signing",
			Required: true,
			Sources:  cli.NewValueSourceChain(cli.EnvVar("SEARCH_SECRET")),
		},
		&cli.StringFlag{
			Name:    fLogLevel,
			Usage:   "Log level (debug, info, warn, error)",
			Value:   "info",
			Sources: cli.NewValueSourceChain(cli.EnvVar("SEARCH_LOG_LEVEL")),
		},
	}
}
