package main

import "github.com/urfave/cli/v3"

var (
	fPort     = "port"
	fDB       = "db"
	fPearHost = "pear-host"
	fM2MToken = "m2m-token"
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
			Name:     fM2MToken,
			Usage:    "Instance-wide M2M bearer token for authenticating to pear",
			Required: true,
			Sources:  cli.NewValueSourceChain(cli.EnvVar("SEARCH_M2M_TOKEN")),
		},
	}
}
