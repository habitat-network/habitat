package main

import "github.com/urfave/cli/v3"

var (
	fDB        = "db"
	fPort      = "port"
	fDomain    = "domain"
	fSecret    = "secret"
	fOrgHandle = "org-handle"
	fLogLevel  = "log-level"
)

func getFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    fDB,
			Usage:   "Database connection string (sqlite:// only)",
			Value:   "sqlite://home.db",
			Sources: cli.EnvVars("HOME_DB"),
		},
		&cli.StringFlag{
			Name:    fPort,
			Usage:   "HTTP server port",
			Value:   "2600",
			Sources: cli.EnvVars("HOME_PORT"),
		},
		&cli.StringFlag{
			Name:    fDomain,
			Usage:   "Publicly-accessible domain of this home instance (its did:web host)",
			Value:   "home.local.habitat.network",
			Sources: cli.EnvVars("HOME_DOMAIN"),
		},
		&cli.StringFlag{
			Name:    fSecret,
			Usage:   "Secret used in the OAuth client-metadata signing",
			Value:   "secret",
			Sources: cli.EnvVars("HOME_SECRET"),
		},
		&cli.StringFlag{
			Name:    fOrgHandle,
			Usage:   "Handle of the org this home server manages groups for",
			Value:   "acmecorp.pear.local.habitat.network",
			Sources: cli.EnvVars("HOME_ORG_HANDLE"),
		},
		&cli.StringFlag{
			Name:    fLogLevel,
			Usage:   "Log level (debug, info, warn, error)",
			Value:   "info",
			Sources: cli.EnvVars("HOME_LOG_LEVEL"),
		},
	}
}
