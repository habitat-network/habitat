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
	fAutoMigrate        = "auto_migrate"
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
	fBlobBucket         = "blob_bucket"
	fNangoSecretKey     = "nango_secret_key"
	fMeilisearchURL     = "meilisearch_url"
	fMeilisearchAPIKey  = "meilisearch_api_key"
	fMeilisearchIndex   = "meilisearch_index"
	fEmbedderURL        = "embedder_url"
	fEmbedderAPIKey     = "embedder_api_key"
	fEmbedderModel      = "embedder_model"
	fEmbedderDimensions = "embedder_dimensions"
	fSemanticRatio      = "search_semantic_ratio"
	fSearchMinScore     = "search_min_score"
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
			Name:     fSpaceSigningKey,
			Usage:    "Multibase-encoded P-256 private key for the single space-host identity. Signs permissioned-repo commits for repo owners on external PDSes",
			Sources:  getSources(fSpaceSigningKey),
			Required: true,
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
		&cli.BoolFlag{
			Name:    fAutoMigrate,
			Usage:   "Apply pending database migrations when the server starts. Turn off when the migrate command runs them separately, e.g. as a Cloud Run job.",
			Value:   true,
			Sources: getSources(fAutoMigrate),
		},
		&cli.StringFlag{
			Name:    fBlobBucket,
			Usage:   "gocloud.dev bucket connection string for blob storage, with inline credentials (e.g. s3://bucket?region=us-east-1, gs://bucket, file:///var/blobs). Defaults to an in-memory store.",
			Value:   "mem://",
			Sources: getSources(fBlobBucket),
		},
		&cli.StringFlag{
			Name:    fNangoSecretKey,
			Usage:   "Secret key for the Nango environment brokering MCP server OAuth connections. If unset, MCP server configuration is unavailable.",
			Sources: getSources(fNangoSecretKey),
		},
		&cli.StringFlag{
			Name:    fMeilisearchURL,
			Usage:   "URL of the Meilisearch server that indexes space records for search. If unset, search is unavailable.",
			Sources: getSources(fMeilisearchURL),
		},
		&cli.StringFlag{
			Name:    fMeilisearchAPIKey,
			Usage:   "API key for the Meilisearch server",
			Sources: getSources(fMeilisearchAPIKey),
		},
		&cli.StringFlag{
			Name:    fMeilisearchIndex,
			Usage:   "Name of the Meilisearch index of space records; cursors go in <name>_cursors",
			Value:   "space_records",
			Sources: getSources(fMeilisearchIndex),
		},
		&cli.StringFlag{
			Name:    fEmbedderURL,
			Usage:   "OpenAI-compatible embeddings endpoint Meilisearch calls to embed records and queries for hybrid search (e.g. http://localhost:11434/v1/embeddings for Ollama, https://<resource>.openai.azure.com/openai/v1/embeddings for Microsoft Foundry). It must be reachable from the Meilisearch server. If unset, search is keyword only.",
			Sources: getSources(fEmbedderURL),
		},
		&cli.StringFlag{
			Name:    fEmbedderAPIKey,
			Usage:   "API key sent as a bearer token to the embeddings endpoint",
			Sources: getSources(fEmbedderAPIKey),
		},
		&cli.StringFlag{
			Name:    fEmbedderModel,
			Usage:   "Embedding model to request; for Microsoft Foundry, the deployment name",
			Sources: getSources(fEmbedderModel),
		},
		&cli.IntFlag{
			Name:    fEmbedderDimensions,
			Usage:   "Size of the model's embeddings. If unset, Meilisearch infers it.",
			Sources: getSources(fEmbedderDimensions),
		},
		&cli.FloatFlag{
			Name:    fSemanticRatio,
			Usage:   "With an embedder, how much hybrid search weighs semantic over keyword matches, from 0 (keyword only) to 1 (semantic only)",
			Value:   0.5,
			Sources: getSources(fSemanticRatio),
		},
		&cli.FloatFlag{
			Name:    fSearchMinScore,
			Usage:   "With an embedder, drop hybrid search hits ranked below this score, from 0 to 1. Semantic search ranks every record, so without it unrelated records trail the results. A good value depends on the model. 0 keeps every hit.",
			Sources: getSources(fSearchMinScore),
		},
	}
}

func getSources(name string) cli.ValueSourceChain {
	return cli.NewValueSourceChain(
		cli.EnvVar("HABITAT_" + strings.ToUpper(name)),
	)
}
