package main

import (
	"github.com/urfave/cli/v3"
)

var fPearDomain = "pear_domain"
var fSecret = "secret"
var fMachineName = "machine_name"

func getFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     fPearDomain,
			Required: true,
			Usage:    "The domain of the pear server",
			Sources:  cli.EnvVars("HABITAT_DOMAIN"),
		},
		&cli.StringFlag{
			Name:     fSecret,
			Required: true,
			Usage:    "The secret of the oauth client",
			Sources:  cli.EnvVars("HABITAT_OAUTH_CLIENT_SECRET"),
		},
		&cli.StringFlag{
			Name:  fMachineName,
			Usage: "The machine name of the oauth client",
			Value: "oauth-client-metadata",
		},
	}
}
