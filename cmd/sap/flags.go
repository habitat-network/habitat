package main

import (
	"github.com/habitat-network/habitat/pkg/sap"
	"github.com/urfave/cli/v3"
)

var (
	fDB                 = "db"
	fPort               = "port"
	fInternalPort       = "internal-port"
	fDomain             = "domain"
	fLogLevel           = "log-level"
	fSecret             = "secret"
	fInternalAuthSecret = "internal-auth-secret"

	fIdentityResolver = "identity-resolver"
	fWebhookURL       = "webhook-url"

	fClientName = "client-name"
	fClientURI  = "client-uri"

	fServiceName = "service-name"

	fOAuthScopes = "oauth-scopes"
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
			Usage:   "Internal HTTP port serving the org and channel endpoints. If set to the same value as -port, the internal routes are served on the same listener as the public ones.",
			Value:   "2581",
			Sources: cli.EnvVars("SAP_INTERNAL_PORT"),
		},
		&cli.StringFlag{
			Name:    fInternalAuthSecret,
			Usage:   "If set, require HTTP basic auth (any username, this value as the password) on the internal routes",
			Sources: cli.EnvVars("SAP_INTERNAL_AUTH_SECRET"),
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
			Name: fIdentityResolver,
			Usage: "Base URL of an atproto identity service (e.g. a Habitat instance) to resolve " +
				"identities through instead of the public network",
			Sources: cli.EnvVars("SAP_IDENTITY_RESOLVER"),
		},
		&cli.StringFlag{
			Name:    fSecret,
			Usage:   "Secret used in OAuth flow",
			Value:   "secret",
			Sources: cli.EnvVars("SAP_SECRET"),
		},
		&cli.StringFlag{
			Name: fWebhookURL,
			Usage: "If set, POST each outbox message to this URL as it's synced, retrying " +
				"a failed delivery with exponential backoff until it succeeds",
			Sources: cli.EnvVars("SAP_WEBHOOK_URL"),
		},
		&cli.StringFlag{
			Name:    fClientName,
			Usage:   "OAuth client name",
			Value:   "sap",
			Sources: cli.EnvVars("SAP_CLIENT_NAME"),
		},
		&cli.StringFlag{
			Name:    fClientURI,
			Usage:   "OAuth client uri",
			Sources: cli.EnvVars("SAP_CLIENT_URI"),
		},
		&cli.StringFlag{
			Name: fServiceName,
			Usage: "Service name this instance publishes its notifyWrite service under in the " +
				"DID document at /.well-known/did.json. Space hosts resolve the service " +
				"identifier (did:web:<domain>#<this value>) to sap's delivery endpoint, so it " +
				"has to match the service entry sap serves",
			Value:   sap.DefaultServiceName,
			Sources: cli.EnvVars("SAP_SERVICE_NAME"),
		},
		&cli.StringSliceFlag{
			Name:    fOAuthScopes,
			Usage:   "OAuth scopes requested by the client (passed to the oauth client config)",
			Value:   []string{"atproto"},
			Sources: cli.EnvVars("SAP_OAUTH_SCOPES"),
		},
	}
}
