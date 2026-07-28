package main

import "github.com/urfave/cli/v3"

var (
	fDB           = "db"
	fPort         = "port"
	fInternalPort = "internal-port"
	fDomain       = "domain"
	fLogLevel     = "log-level"
	fSecret       = "secret"
)

func getFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    fDB,
			Usage:   "Database connection string",
			Value:   "sqlite://sap.db",
			Sources: cli.EnvVars("SAP_DB"),
		},
		&cli.StringFlag{
			Name:    fPort,
			Usage:   "Public HTTP port serving the OAuth endpoints (callback, client metadata)",
			Value:   "2580",
			Sources: cli.EnvVars("SAP_PORT"),
		},
		&cli.StringFlag{
			Name:    fInternalPort,
			Usage:   "Internal HTTP port serving the org and channel endpoints",
			Value:   "2581",
			Sources: cli.EnvVars("SAP_INTERNAL_PORT"),
		},
		&cli.StringFlag{
			Name:    fDomain,
			Usage:   "Publicly-accessible domain of this SAP instance",
			Value:   "sap.local.habitat.network",
			Sources: cli.EnvVars("SAP_DOMAIN"),
		},
		&cli.StringFlag{
			Name:    fLogLevel,
			Usage:   "Log level (debug, info, warn, error)",
			Value:   "info",
			Sources: cli.EnvVars("SAP_LOG_LEVEL"),
		},
		&cli.StringFlag{
			Name:    fSecret,
			Usage:   "Secret used in OAuth flow",
			Value:   "secret",
			Sources: cli.EnvVars("SAP_SECRET"),
		},
	}
}
