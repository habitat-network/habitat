package main

import (
	"strings"

	"github.com/urfave/cli/v3"
)

var (
	fDebug              = "debug"
	fDomain             = "domain"
	fServiceName        = "service_name"
	fDB                 = "db"
	fPort               = "port"
	fHttpsCerts         = "httpscerts"
	fPdsCredEncryptKey  = "pds_cred_encrypt_key"
	fSpaceSigningKey    = "space_signing_key"
	fOauthServerSecret  = "oauth_server_secret"
	fOauthClientSecret  = "oauth_client_secret"
	fHiveDomain         = "hive_domain"
	fGoogleClientID     = "google_client_id"
	fGoogleClientSecret = "google_client_secret"
	fPdsOauthClientUri  = "pds_oauth_client_uri"
	fAdminPassword      = "admin_password"
	fUiDevProxy         = "ui_dev_proxy"
	fBuiltinApps        = "builtin_app"
)

var profiles []string

func getFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:    fDebug,
			Usage:   "Enable debug mode",
			Sources: getSources(fDebug),
		},
		&cli.StringSliceFlag{
			Name:        "profile",
			Usage:       "YAML profile files that specify flags. Can be stacked from highest precedence to lowest.",
			TakesFile:   true,
			Destination: &profiles,
		},
		&cli.StringFlag{
			Name:     fDomain,
			Required: true,
			Usage:    "The publicly available domain at which the server can be found",
			Sources:  getSources(fDomain),
		},
		&cli.StringFlag{
			Name:        fServiceName,
			Usage:       "The service name of habitat that should be looked up in users' DID doc services list",
			DefaultText: "habitat",
			Sources:     getSources(fServiceName),
		},
		&cli.StringFlag{
			Name:    fPort,
			Usage:   "The port on which to run the server",
			Value:   "8000",
			Sources: getSources(fPort),
		},
		&cli.StringFlag{
			Name:    fHttpsCerts,
			Usage:   "The directory in which TLS certs can be found. Should contain fullchain.pem and privkey.pem",
			Sources: getSources(fHttpsCerts),
		},
		&cli.StringFlag{
			Name:     fPdsCredEncryptKey,
			Usage:    "32-byte base64-encoded encryption key for PDS credentials. Can use cmd/keygen to generate",
			Required: true,
			Sources:  getSources(fPdsCredEncryptKey),
		},
		&cli.StringFlag{
			Name:    fSpaceSigningKey,
			Usage:   "Multibase-encoded P-256 private key for the single space-host identity. Signs permissioned-repo commits for repo owners on external PDSes. If unset, host-signed commits are omitted.",
			Sources: getSources(fSpaceSigningKey),
		},
		&cli.StringFlag{
			Name:     fOauthServerSecret,
			Usage:    "32-byte base64-encoded secret for the OAuth server. Can use cmd/keygen to generate",
			Required: true,
			Sources:  getSources(fOauthServerSecret),
		},
		&cli.StringFlag{
			Name:     fOauthClientSecret,
			Usage:    "32-byte base64-encoded secret for the OAuth client. Can use cmd/keygen to generate",
			Required: true,
			Sources:  getSources(fOauthClientSecret),
		},
		&cli.StringFlag{
			Name:    fHiveDomain,
			Usage:   "The domain at which the hive hosts identities",
			Sources: getSources(fHiveDomain),
		},
		&cli.StringFlag{
			Name:    fGoogleClientID,
			Usage:   "Google OAuth client ID for Google Sign-In login method",
			Sources: getSources(fGoogleClientID),
		},
		&cli.StringFlag{
			Name:    fGoogleClientSecret,
			Usage:   "Google OAuth client secret for Google Sign-In login method",
			Sources: getSources(fGoogleClientSecret),
		},
		&cli.StringFlag{
			Name:    fPdsOauthClientUri,
			Usage:   "PDS OAuth client ID for PDS login method",
			Sources: getSources(fPdsOauthClientUri),
		},
		&cli.StringFlag{
			Name:    fAdminPassword,
			Usage:   "Preset password for the instance admin account; if unset, a random password is generated on every boot and printed once. Not persisted - kept in memory only",
			Sources: getSources(fAdminPassword),
		},
		&cli.StringFlag{
			Name:    fUiDevProxy,
			Usage:   "If set, reverse-proxy the embedded /ui/ pages to this URL (the pear-pages dev server) instead of serving the embedded build. Used in development.",
			Sources: getSources(fUiDevProxy),
		},
		&cli.StringSliceFlag{
			Name:    fBuiltinApps,
			Usage:   "Builtin clients that can retrieve instance token using jwt bearer grants",
			Sources: getSources(fBuiltinApps),
		},

		&cli.StringFlag{
			Name:    fDB,
			Usage:   "Database connection string",
			Value:   "sqlite://repo.db",
			Sources: getSources(fDB),
		},
	}
}

func getSources(name string) cli.ValueSourceChain {
	return cli.NewValueSourceChain(
		cli.EnvVar("HABITAT_" + strings.ToUpper(name)),
	)
}
