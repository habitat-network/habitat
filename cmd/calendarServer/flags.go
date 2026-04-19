package main

import (
	"github.com/urfave/cli/v3"
)

var (
	fPort               = "port"
	fDomain             = "domain"
	fGoogleClientID     = "google-client-id"
	fGoogleClientSecret = "google-client-secret"
	fDB                 = "db"
)

func getFlags() []cli.Flag {
	return []cli.Flag{
		&cli.IntFlag{
			Name:    fPort,
			Usage:   "Port to run the server on",
			Value:   8080,
			Sources: cli.NewValueSourceChain(cli.EnvVar("CALENDAR_PORT")),
		},
		&cli.StringFlag{
			Name:     fDomain,
			Usage:    "Domain that this server is running on",
			Required: true,
			Sources:  cli.NewValueSourceChain(cli.EnvVar("CALENDAR_DOMAIN")),
		},
		&cli.StringFlag{
			Name:     fGoogleClientID,
			Usage:    "Google OAuth2 client ID",
			Required: true,
			Sources:  cli.NewValueSourceChain(cli.EnvVar("CALENDAR_GOOGLE_CLIENT_ID")),
		},
		&cli.StringFlag{
			Name:     fGoogleClientSecret,
			Usage:    "Google OAuth2 client secret",
			Required: true,
			Sources:  cli.NewValueSourceChain(cli.EnvVar("CALENDAR_GOOGLE_CLIENT_SECRET")),
		},
		&cli.StringFlag{
			Name:    fDB,
			Usage:   "Path to SQLite database for session storage",
			Value:   "./calendar.db",
			Sources: cli.NewValueSourceChain(cli.EnvVar("CALENDAR_DB")),
		},
	}
}

func validateFlags(cmd *cli.Command) error {
	return nil
}
