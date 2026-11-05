package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/clientmetadata"
	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_identity "github.com/habitat-network/habitat/internal/identity"
	"github.com/habitat-network/habitat/internal/keyval"
	"github.com/habitat-network/habitat/internal/llmgateway"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/notify"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pearapp"
	"github.com/habitat-network/habitat/internal/pearserver"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/searchconfig"
	"github.com/habitat-network/habitat/internal/simplespace"

	"github.com/habitat-network/habitat/internal/pdsoauth"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/urfave/cli/v3"
	"gocloud.dev/blob"
)

// searchIndexName names the Meilisearch index of space records; their
// cursors are in <name>_cursors.
const searchIndexName = "space_records"

// searchIndexOptions returns the settings of the search index the flags
// describe.
func searchIndexOptions(cmd *cli.Command) []search.MeilisearchOption {
	embedderURL := cmd.String(fEmbedderURL)
	if embedderURL == "" {
		return nil
	}
	// With an embedder, Meilisearch embeds records and queries itself and
	// searches are hybrid.
	return []search.MeilisearchOption{search.WithEmbedder(search.Embedder{
		URL:           embedderURL,
		APIKey:        cmd.String(fEmbedderAPIKey),
		Model:         cmd.String(fEmbedderModel),
		Dimensions:    int(cmd.Int(fEmbedderDimensions)),
		SemanticRatio: cmd.Float(fSemanticRatio),
		MinScore:      cmd.Float(fSearchMinScore),
	})}
}

// setupComponents builds pear's App from the command's flags. It
// doesn't start anything: the server, migrations and tasks decide what to run.
func setupComponents(ctx context.Context, cmd *cli.Command) (*pearapp.App, error) {
	meter := otel.Meter("habitat-meter")

	// The gorm handle is named database so that db keeps referring to
	// internal/db.
	database, err := db.New(cmd.String(fDB))
	if err != nil {
		return nil, fmt.Errorf("setup database: %w", err)
	}
	fgaStore, err := setupFGA(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("setup fga store: %w", err)
	}

	oauthSecret, err := encrypt.ParseKey(cmd.String(fOauthServerSecret))
	if err != nil {
		return nil, fmt.Errorf("parse oauth server secret for login provider: %w", err)
	}

	// Requires the migrations (run by `pear migrate`) to have created the table.
	// Failing here is fatal: a store that silently failed to build would turn
	// every Put into a runtime error later.
	kv, err := keyval.New(ctx, database)
	if err != nil {
		return nil, fmt.Errorf("setup keyval store: %w", err)
	}

	credKey, err := encrypt.ParseKey(cmd.String(fPdsCredEncryptKey))
	if err != nil {
		return nil, fmt.Errorf("load PDS encryption key: %w", err)
	}
	encryptedKV, err := keyval.NewEncrypted(kv, credKey)
	if err != nil {
		return nil, fmt.Errorf("setup encrypted keyval store: %w", err)
	}
	pdsOAuthStore := pdsoauth.NewStore(
		encryptedKV.Namespace("pds_oauth_sessions"),
		kv.Namespace("pds_oauth_sessions"),
		kv.Namespace("pds_oauth_requests"),
	)

	domain := cmd.String(fDomain)
	var clientUri string
	if cmd.String(fPdsOauthClientUri) != "" {
		clientUri = "https://" + cmd.String(fPdsOauthClientUri)
	}
	if clientUri == "" {
		clientUri = "https://" + domain
	}
	hiveDomain := cmd.String(fHiveDomain)
	if hiveDomain == "" {
		hiveDomain = domain
	}

	hive, err := hive.NewHive(hiveDomain, domain, database.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("setup hive (identity service for org): %w", err)
	}
	// Be careful about where this is passed, because only privileged services that are doing auth
	// should be able to fallback to the hive directory implementation
	defaultDir := identity.DefaultDirectory()
	// hive is the base directory (tried first) since it resolves DIDs under our
	// own domain locally; falling back to defaultDir's network resolution first
	// would make this server make an outbound HTTP request back to itself for
	// any locally-hosted DID.
	hiveDir := habitat_identity.NewWrappedDirectory(hive, defaultDir)

	oauthClient, err := pdsoauth.NewApp(
		pdsoauth.Config{
			ClientID:    clientUri + "/client-metadata.json",
			ClientURI:   clientUri,
			CallbackURL: "https://" + domain + "/oauth-callback",
			Secret:      cmd.String(fOauthClientSecret),
		},
		pdsOAuthStore,
		defaultDir,
	)
	if err != nil {
		return nil, fmt.Errorf("setup oauth client: %w", err)
	}
	pdsClientFactory, err := pdsoauth.NewHttpClientFactory(oauthClient)
	if err != nil {
		return nil, fmt.Errorf("setup PDS client factory: %w", err)
	}

	emailDomainStore, err := emaildomain.NewStore(database.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("setup email domain store: %w", err)
	}

	loginRouter := &org.LoginRouter{
		Pds:        login.NewPDSProvider(oauthClient),
		EmailStore: emailDomainStore,
	}
	googleClientID := cmd.String(fGoogleClientID)
	googleClientSecret := cmd.String(fGoogleClientSecret)
	if googleClientID != "" && googleClientSecret != "" {
		googleProvider, err := login.NewGoogleProvider(
			googleClientID,
			googleClientSecret,
			"https://"+domain+"/oauth-callback",
			encryptedKV.Namespace("google_credentials"),
			kv.Namespace("login"),
		)
		if err != nil {
			return nil, fmt.Errorf("setup google login provider: %w", err)
		}
		loginRouter.Google = googleProvider
		slog.InfoContext(ctx, "google login provider enabled")
	}

	// Habitat's single host signing key signs delegation tokens and space
	// credentials.
	hostKey, err := atcrypto.ParsePrivateMultibase(cmd.String(fSpaceSigningKey))
	if err != nil {
		return nil, fmt.Errorf("parse space-host signing key: %w", err)
	}
	// The host's KEM key keys permissioned-repo commit MACs for repo owners on
	// external PDSes (habitat-managed owners use their own hive KEM key
	// instead). It is derived from the host key rather than configured, so it
	// needs no secret of its own, but is a dedicated key all the same.
	hostKEMKey, err := spacecommit.DeriveKEMKey(hostKey)
	if err != nil {
		return nil, fmt.Errorf("derive host kem key: %w", err)
	}

	notifyStore, err := notify.NewStore(database.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("setup notify store: %w", err)
	}
	notifier := notify.NewNotifier(notifyStore, httpx.NewClient(), hive)

	// With Meilisearch configured, the search indexer follows space writes
	// like any other syncer, so the spaces store notifies it alongside the
	// registered ones.
	notifiers := spaces.Notifiers{notifier}
	blobBucket, err := blob.OpenBucket(ctx, cmd.String(fBlobBucket))
	if err != nil {
		return nil, fmt.Errorf("open blob bucket: %w", err)
	}
	blobStore := spaces.NewBlobStore(blobBucket)

	// pear opens the search index as it is; `pear run reindex` rebuilds it
	// when its settings or the embedder change.
	var searchIndex search.Index
	var searchIndexer *search.Indexer
	if meilisearchURL := cmd.String(fMeilisearchURL); meilisearchURL != "" {
		searchIndex, err = search.OpenMeilisearch(
			ctx,
			meilisearchURL,
			cmd.String(fMeilisearchAPIKey),
			searchIndexName,
			searchIndexOptions(cmd)...,
		)
		if err != nil {
			return nil, fmt.Errorf("setup search index: %w", err)
		}
		searchIndexer = search.NewIndexer(searchIndex, search.WithBlobs(blobStore))
		notifiers = append(notifiers, searchIndexer)
	}

	spacesStore, err := spaces.NewStore(
		database.WithContext(ctx),
		notifiers,
	)
	if err != nil {
		return nil, fmt.Errorf("setup spaces store: %w", err)
	}

	opensocialStore, err := opensocial.NewStore(
		database.WithContext(ctx), spacesStore, blobStore, hive,
	)
	if err != nil {
		return nil, fmt.Errorf("setup opensocial store: %w", err)
	}
	loginRouter.OpensocialStore = opensocialStore
	emailResolver := habitat_identity.NewEmailResolver(
		database.WithContext(ctx), emailDomainStore, hive,
	)
	loginRouter.OpensocialStore = opensocialStore

	// The MCP endpoints sign users in through this same loginRouter (see
	// OAuthServer.HandleMCPAuthorizeSubmit): whichever login method a user's
	// org configures (PDS, Google, or password) is what an MCP client's login
	// runs too, landing back at the one /oauth-callback those providers are
	// registered with. Identity is resolved through pear's own resolveIdentity
	// endpoint (see api-docs/docs/space-proxy/getting-started.mdx), which
	// already rewrites pear-hosted accounts' PDS pointer to pear itself — so
	// those accounts redirect to pear's own atproto OAuth server, and remote
	// accounts resolve exactly the way any other client following that doc
	// would see them.
	mcpOrigin := "https://" + domain

	oauthServer, err := oauthserver.NewOAuthServer(
		oauthSecret,
		loginRouter,
		// OAuth server needs privileged access to lookup hive-hosted identities
		hiveDir,
		database.WithContext(ctx),
		kv.Namespace("oauth"),
		meter,
		"https://"+domain,
		oauthserver.NewJWTBearerStore(
			cmd.StringSlice(fBuiltinApps)...,
		),
		opensocialStore,
		emailResolver,
		oauthserver.CookieDomain(cmd.String(fLoginHintCookieDomain)),
	)
	if err != nil {
		return nil, fmt.Errorf("setup oauth server: %w", err)
	}

	serviceAuth := authn.NewServiceAuthMethod(
		defaultDir,
		syntax.DID("did:web:"+domain),
		"https://"+domain,
	)

	permStore := perms.NewStore(database, spacesStore, fgaStore, opensocialStore)
	credentialRevocations := authn.NewCredentialRevocations(
		kv.Namespace("space_credential_revoked"),
	)
	spaceCredential := authn.NewSpaceCredentialAuthMethod(defaultDir, credentialRevocations)
	validator := authn.NewValidator(
		oauthServer,
		serviceAuth,
		spaceCredential,
		permStore,
	)

	simpleStore := simplespace.NewStore(
		database,
		spacesStore,
		permStore,
		syntax.DID("did:web:"+domain),
	)

	// Store for org-configured MCP servers and members' OAuth tokens for them.
	// Org admins choose the servers' URLs, so OAuth discovery and token
	// requests only reach public addresses unless allowed for development.
	mcpClient := httpx.NewPublicClient()
	if cmd.Bool(fMCPAllowPrivateNets) {
		mcpClient = httpx.NewClient()
	}
	mcpGatewayStore, err := mcpgateway.NewStore(
		database,
		encryptedKV,
		kv,
		credKey,
		opensocialStore,
		mcpClient,
		"https://"+domain+pearserver.MCPOAuthCallbackPath,
		mcpgateway.WithPresetClients(map[mcpgateway.Preset]mcpgateway.PresetClient{
			mcpgateway.PresetGoogleDrive: {
				ID:     cmd.String(fMCPGoogleDriveID),
				Secret: cmd.String(fMCPGoogleDriveSecret),
			},
			mcpgateway.PresetSlack: {
				ID:     cmd.String(fMCPSlackID),
				Secret: cmd.String(fMCPSlackSecret),
			},
			mcpgateway.PresetGitHub: {
				ID:     cmd.String(fMCPGitHubID),
				Secret: cmd.String(fMCPGitHubSecret),
			},
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("setup mcp gateway store: %w", err)
	}

	llmStore, err := llmgateway.NewStore(
		database,
		credKey,
		llmgateway.NewModelFactory(httpx.NewClient()),
	)
	if err != nil {
		return nil, fmt.Errorf("setup llm gateway store: %w", err)
	}

	pdsForwarding := forwarding.NewPDSForwarding(
		validator,
		pdsClientFactory,
		defaultDir,
	)

	searchConfigStore := searchconfig.NewStore(spacesStore)
	var searcher *search.Searcher
	if searchIndex != nil {
		searcher = search.NewSearcher(
			searchIndex, opensocialStore, spacesStore,
			search.WithCollections(searchConfigStore),
		)
	}

	return pearapp.New(
		database,
		domain,
		hiveDomain,
		mcpOrigin,
		oauthClient,
		hive,
		defaultDir,
		hiveDir,
		pdsClientFactory,
		emailDomainStore,
		hostKey,
		hostKEMKey,
		notifyStore,
		notifier,
		blobBucket,
		blobStore,
		spacesStore,
		opensocialStore,
		emailResolver,
		oauthServer,
		serviceAuth,
		permStore,
		validator,
		credentialRevocations,
		kv.Namespace("space_delegation_spent"),
		simpleStore,
		mcpGatewayStore,
		llmStore,
		pdsForwarding,
		clientmetadata.NewResolver(),
		searchIndex,
		searchIndexer,
		searcher,
		searchConfigStore,
	), nil
}

func setupFGA(ctx context.Context, cmd *cli.Command) (fgastore.Store, error) {
	dsn := cmd.String(fDB)
	// Share the main Postgres database for FGA when one is configured; only fall
	// back to a separate SQLite file when the main store is SQLite.
	if db.ParseDialect(dsn) == db.Postgres {
		dsn, err := db.EnsureUTF8ClientEncoding(dsn)
		if err != nil {
			return nil, err
		}
		fga, err := fgastore.NewPostgres(ctx, dsn)
		if err != nil {
			return nil, fmt.Errorf("setup fga store with postgres: %w", err)
		}
		return fga, nil
	}
	// Use a separate SQLite file for FGA to avoid lock conflicts between
	// mattn/go-sqlite3 (used by GORM) and modernc.org/sqlite (used by OpenFGA).
	// Strip the "sqlite://" scheme (as internal/db does) so we hand OpenFGA a
	// plain filesystem path rather than a URI it parses as a host.
	fgaPath := strings.TrimPrefix(cmd.String(fDB), "sqlite://") + ".fga.db"
	fga, err := fgastore.NewSQLite(ctx, fgaPath)
	if err != nil {
		return nil, fmt.Errorf("setup fga sqlite store %q: %w", fgaPath, err)
	}
	return fga, nil
}
