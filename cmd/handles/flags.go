package main

import "github.com/urfave/cli/v3"

var (
	fDb           = "db"
	fPgUrl        = "pgurl"
	fPort         = "port"
	fHandleDomain = "handle-domain"
)

func getFlags() ([]cli.Flag, []cli.MutuallyExclusiveFlags) {
	return []cli.Flag{
			&cli.StringFlag{
				Name:  fPort,
				Usage: "The port on which to run the server",
				Value: "8000",
			},
			&cli.StringFlag{
				Name:  fHandleDomain,
				Usage: "Handle domain",
			},
		}, []cli.MutuallyExclusiveFlags{
			{
				Flags: [][]cli.Flag{
					{
						&cli.StringFlag{
							Name:  fDb,
							Usage: "The path to the sqlite file to use as the backing database",
							Value: "./repo.db",
						},
					},
					{
						&cli.StringFlag{
							Name:  fPgUrl,
							Usage: "The postgres connection string",
						},
					},
				},
			},
		}
}
