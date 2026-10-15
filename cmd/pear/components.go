package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
	"go.opentelemetry.io/otel"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/clientmetadata"
	"github.com/habitat-network/habitat/internal/clique"
	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_identity "github.com/habitat-network/habitat/internal/identity"
	"github.com/habitat-network/habitat/internal/instance"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/nango"
	"github.com/habitat-network/habitat/internal/notify"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pearapp"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/searchconfig"
	"github.com/habitat-network/habitat/internal/simplespace"

	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
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

	passwordHash, err := setupInstanceAdminPassword(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("setup instance admin password: %w", err)
	}
	// Reuse the oauth server secret
	instanceAdminStore, err := instance.NewStore(
		database.WithContext(ctx),
		oauthSecret,
		fDomain,
		passwordHash,
	)
	if err != nil {
		return nil, fmt.Errorf("setup instance admin store: %w", err)
	}

	instanceAdminServer := instance.NewServer(instanceAdminStore, "habitat.network")

	credKey, err := encrypt.ParseKey(cmd.String(fPdsCredEncryptKey))
	if err != nil {
		return nil, fmt.Errorf("load PDS encryption key: %w", err)
	}
	pdsCredStore, err := pdscred.NewPDSCredentialStore(database.WithContext(ctx), credKey)
	if err != nil {
		return nil, fmt.Errorf("setup pds cred store: %w", err)
	}

	domain := cmd.String(fDomain)
	var clientUri string
	if cmd.String(fPdsOauthClientUri) != "" {
		clientUri = "https://" + cmd.String(fPdsOauthClientUri)
	}
	if clientUri == "" {
		clientUri = "https://" + domain
	}
	oauthClient, err := pdsclient.NewPdsOAuthClient(
		clientUri+"/client-metadata.json",
		clientUri,
		"https://"+domain+"/oauth-callback",
		cmd.String(fOauthClientSecret),
		meter,
	)
	if err != nil {
		return nil, fmt.Errorf("setup oauth client: %w", err)
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

	pdsClientFactory, err := pdsclient.NewHttpClientFactory(
		pdsCredStore,
		oauthClient,
		defaultDir,
	)
	if err != nil {
		return nil, fmt.Errorf("setup PDS client factory: %w", err)
	}

	passwordProvider, err := login.NewPasswordProvider(
		database,
		cmd.String(fDomain),
		oauthSecret,
		hiveDir,
	)
	if err != nil {
		return nil, fmt.Errorf("setup password login provider: %w", err)
	}
	everyoneOrg := org.NewEveryoneOrg(domain)
	orgStore, err := org.NewStore(
		database.WithContext(ctx),
		hive,
		hiveDir,
		domain,
		passwordProvider,
		fgaStore,
		everyoneOrg,
	)
	if err != nil {
		return nil, fmt.Errorf("setup org store: %w", err)
	}

	emailDomainStore, err := emaildomain.NewStore(database.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("setup email domain store: %w", err)
	}

	loginRouter := &org.LoginRouter{
		Pds:        login.NewPDSProvider(oauthClient, pdsCredStore, defaultDir),
		Password:   passwordProvider,
		OrgStore:   orgStore,
		EmailStore: emailDomainStore,
	}
	googleClientID := cmd.String(fGoogleClientID)
	googleClientSecret := cmd.String(fGoogleClientSecret)
	if googleClientID != "" && googleClientSecret != "" {
		googleProvider, err := login.NewGoogleProvider(
			googleClientID,
			googleClientSecret,
			"https://"+domain+"/oauth-callback",
			database.WithContext(ctx),
			credKey,
		)
		if err != nil {
			return nil, fmt.Errorf("setup google login provider: %w", err)
		}
		loginRouter.Google = googleProvider
		slog.InfoContext(ctx, "google login provider enabled")
	}

	// Habitat's single host signing key signs permissioned-repo commits for repo
	// owners on external PDSes (habitat-managed owners sign with their own hive
	// key instead). Optional: if unset, host-signed commits are omitted.
	hostKey, err := atcrypto.ParsePrivateMultibase(cmd.String(fSpaceSigningKey))
	if err != nil {
		return nil, fmt.Errorf("parse space-host signing key: %w", err)
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
		spacecommit.NewAuthority(hostKey, hive),
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
		meter,
		orgStore,
		"https://"+domain,
		oauthserver.NewJWTBearerStore(
			cmd.StringSlice(fBuiltinApps)...,
		),
		opensocialStore,
		emailResolver,
	)
	if err != nil {
		return nil, fmt.Errorf("setup oauth server: %w", err)
	}
	oauthGC := oauthserver.NewCollector(database.WithContext(ctx), 5*time.Minute)

	serviceAuth := authn.NewServiceAuthMethod(
		everyoneOrg,
		defaultDir,
		syntax.DID("did:web:"+domain),
		"https://"+domain,
	)

	cliqueStore, err := clique.NewStore(database.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("setup clique store: %w", err)
	}

	permStore := perms.NewStore(database, spacesStore, fgaStore, opensocialStore)
	spaceCredential := authn.NewSpaceCredentialAuthMethod(defaultDir)
	validator := authn.NewValidator(
		oauthServer,
		serviceAuth,
		spaceCredential,
		authn.NewDelegationTokenAuthMethod(hiveDir, permStore, hostKey),
		permStore,
	)

	simpleStore := simplespace.NewStore(database, spacesStore, permStore)

	// Store for org-configured MCP servers and per-user Nango connections.
	nangoSecretKey := cmd.String(fNangoSecretKey)
	if nangoSecretKey == "" {
		slog.WarnContext(ctx, "nango secret key not set; MCP server configuration is disabled")
	}
	nangoClient := nango.NewClient(nangoSecretKey, httpx.NewClient())
	mcpGatewayStore, err := mcpgateway.NewStore(nangoClient, opensocialStore)
	if err != nil {
		return nil, fmt.Errorf("setup mcp gateway store: %w", err)
	}

	pdsForwarding := forwarding.NewPDSForwarding(
		pdsCredStore,
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
			search.WithEveryoneOrg(everyoneOrg.DID()),
		)
	}

	return pearapp.New(
		database,
		instanceAdminStore,
		instanceAdminServer,
		domain,
		hiveDomain,
		mcpOrigin,
		oauthClient,
		hive,
		defaultDir,
		hiveDir,
		pdsClientFactory,
		passwordProvider,
		orgStore,
		emailDomainStore,
		hostKey,
		notifyStore,
		blobBucket,
		blobStore,
		spacesStore,
		opensocialStore,
		emailResolver,
		oauthServer,
		oauthGC,
		serviceAuth,
		cliqueStore,
		permStore,
		validator,
		simpleStore,
		nangoClient,
		mcpGatewayStore,
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

func setupInstanceAdminPassword(ctx context.Context, cmd *cli.Command) (string, error) {
	pass := cmd.String(fAdminPassword)

	// Generate a password on startup if not given
	generate := pass == ""
	if generate {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		pass = string(b)
		slog.WarnContext(
			ctx,
			"generated instance admin password; save it now, it will not be shown again until next restart. password changes on restart if not added to environment variables via HABITAT_ADMIN_PASSWORD",
			"username",
			"admin",
			"password",
			pass,
		)
	}
	passwordHash, err := argon2id.CreateHash(pass, argon2id.DefaultParams)
	if err != nil {
		return "", err
	}
	return passwordHash, nil
}
