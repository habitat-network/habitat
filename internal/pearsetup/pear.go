package pearsetup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/gorilla/mux"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"gocloud.dev/blob"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/clique"
	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_identity "github.com/habitat-network/habitat/internal/identity"
	"github.com/habitat-network/habitat/internal/instance"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/notify"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/org"
	org_server "github.com/habitat-network/habitat/internal/org/server"
	"github.com/habitat-network/habitat/internal/p2p"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/habitat-network/habitat/internal/pear"
	"github.com/habitat-network/habitat/internal/pearsetup/migrations"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/relationship"
	"github.com/habitat-network/habitat/internal/repo"
	"github.com/habitat-network/habitat/internal/simplespace"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_server "github.com/habitat-network/habitat/internal/spaces/server"
	"github.com/habitat-network/habitat/internal/webui"
)

// Pear is a fully wired instance: every store and server the route table
// needs, plus the router they are registered on. Fields are exported so
// tests can seed fixtures directly against the same stores the routes use.
type Pear struct {
	Config Config

	DB            *gorm.DB
	FGA           fgastore.Store
	Hive          hive.Hive
	Directory     identity.Directory
	OrgStore      org.Store
	CliqueStore   clique.Store
	SpacesStore   spaces.Store
	PermStore     perms.Store
	NotifyStore   notify.Store
	Repo          repo.Repo
	Permissions   permissions.Store
	InstanceStore instance.AdminStore
	PDSCredStore  pdscred.PDSCredentialStore
	OAuthServer   *oauthserver.OAuthServer
	Validator     authn.RequestValidator
	HostKey       atcrypto.PrivateKey

	Router *mux.Router

	orgServer          *org_server.Server
	spacesServer       *spaces_server.Server
	notifyServer       *notify.Server
	simplespaceServer  *simplespace.Server
	relationshipServer *relationship.Server
	cliqueServer       *clique.Server
	pearServer         *pear.Server
	p2pServer          *p2p.Server
	idServer           *habitat_identity.Server
	instanceServer     *instance.Server
	pdsForwarding      *forwarding.PDSForwarding
	pdsClientFactory   pdsclient.HttpClientFactory
	oauthClient        pdsclient.PdsOAuthClient
	passwordProvider   *login.PasswordLoginProvider
	oauthGC            *oauthserver.Collector

	bucket        *blob.Bucket
	uiHandler     http.Handler
	hostPublicKey atcrypto.PublicKey
}

// otelMeter returns the process's OpenTelemetry meter. It is a no-op meter
// when telemetry is unset.
func otelMeter() metric.Meter {
	return otel.Meter("habitat-meter")
}

// New builds every component a Pear needs: stores, servers, the OAuth server,
// and the request validator. It does not touch the router beyond creating it
// and registering middleware and routes; see routes.go.
func New(ctx context.Context, cfg Config) (*Pear, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	p := &Pear{Config: cfg}

	gormDB, err := db.New(cfg.DB, db.WithMigrations(migrations.FS))
	if err != nil {
		return nil, fmt.Errorf("setup database: %w", err)
	}
	p.DB = gormDB

	fgaStore := cfg.FGA
	if fgaStore == nil {
		fgaStore, err = setupFGA(ctx, cfg.DB)
		if err != nil {
			return nil, fmt.Errorf("setup fga store: %w", err)
		}
	}
	p.FGA = fgaStore

	instanceStore, err := instance.NewStore(
		gormDB.WithContext(ctx),
		cfg.OAuthServerSecret,
		cfg.Domain,
		cfg.AdminPasswordHash,
	)
	if err != nil {
		return nil, fmt.Errorf("setup instance admin store: %w", err)
	}
	p.InstanceStore = instanceStore
	p.instanceServer = instance.NewServer(instanceStore, "habitat.network")

	pdsCredStore, err := pdscred.NewPDSCredentialStore(gormDB.WithContext(ctx), cfg.PDSCredEncryptKey)
	if err != nil {
		return nil, fmt.Errorf("setup pds cred store: %w", err)
	}
	p.PDSCredStore = pdsCredStore

	oauthClient, err := pdsclient.NewPdsOAuthClient(
		cfg.PDSOAuthClientURI+"/client-metadata.json",
		cfg.PDSOAuthClientURI,
		"https://"+cfg.Domain+"/oauth-callback",
		cfg.OAuthClientSecret,
		otelMeter(),
	)
	if err != nil {
		return nil, fmt.Errorf("setup oauth client: %w", err)
	}
	p.oauthClient = oauthClient

	h, err := hive.NewHive(cfg.HiveDomain, cfg.Domain, gormDB.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("setup hive (identity service for org): %w", err)
	}
	p.Hive = h

	// Be careful about where this is passed, because only privileged services that are doing auth
	// should be able to fallback to the hive directory implementation
	defaultDir := cfg.Directory
	// hive is the base directory (tried first) since it resolves DIDs under our
	// own domain locally; falling back to defaultDir's network resolution first
	// would make this server make an outbound HTTP request back to itself for
	// any locally-hosted DID.
	hiveDir := habitat_identity.NewWrappedDirectory(h, defaultDir)
	p.Directory = hiveDir

	pdsClientFactory, err := pdsclient.NewHttpClientFactory(pdsCredStore, oauthClient, defaultDir)
	if err != nil {
		return nil, fmt.Errorf("setup PDS client factory: %w", err)
	}
	p.pdsClientFactory = pdsClientFactory

	passwordProvider, err := login.NewPasswordProvider(gormDB, cfg.Domain, cfg.OAuthServerSecret, hiveDir)
	if err != nil {
		return nil, fmt.Errorf("setup password login provider: %w", err)
	}
	p.passwordProvider = passwordProvider

	everyoneOrg := org.NewEveryoneOrg(cfg.Domain)
	orgStore, err := org.NewStore(
		gormDB.WithContext(ctx),
		h,
		hiveDir,
		cfg.Domain,
		passwordProvider,
		fgaStore,
		everyoneOrg,
	)
	if err != nil {
		return nil, fmt.Errorf("setup org store: %w", err)
	}
	p.OrgStore = orgStore

	loginRouter := &org.LoginRouter{
		Pds:      login.NewPDSProvider(oauthClient, pdsCredStore, defaultDir),
		Password: passwordProvider,
		OrgStore: orgStore,
	}
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" {
		googleProvider, err := login.NewGoogleProvider(
			cfg.GoogleClientID,
			cfg.GoogleClientSecret,
			"https://"+cfg.Domain+"/oauth-callback",
			gormDB.WithContext(ctx),
			cfg.PDSCredEncryptKey,
		)
		if err != nil {
			return nil, fmt.Errorf("setup google login provider: %w", err)
		}
		loginRouter.Google = googleProvider
	}

	oauthServer, err := oauthserver.NewOAuthServer(
		cfg.OAuthServerSecret,
		loginRouter,
		// OAuth server needs privileged access to lookup hive-hosted identities
		hiveDir,
		gormDB.WithContext(ctx),
		otelMeter(),
		orgStore,
		"https://"+cfg.Domain,
		oauthserver.NewJWTBearerStore(cfg.BuiltinApps...),
	)
	if err != nil {
		return nil, fmt.Errorf("setup oauth server: %w", err)
	}
	p.OAuthServer = oauthServer
	p.oauthGC = oauthserver.NewCollector(gormDB.WithContext(ctx), 5*time.Minute)

	serviceAuth := authn.NewServiceAuthMethod(
		everyoneOrg,
		defaultDir,
		fmt.Sprintf("did:web:%s#habitat", cfg.Domain),
	)

	// Habitat's single host signing key signs permissioned-repo commits for repo
	// owners on external PDSes (habitat-managed owners sign with their own hive
	// key instead). Optional: if unset, host-signed commits are omitted.
	p.HostKey = cfg.SpaceSigningKey

	cliqueStore, err := clique.NewStore(gormDB.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("setup clique store: %w", err)
	}
	p.CliqueStore = cliqueStore

	notifyStore, err := notify.NewStore(gormDB.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("setup notify store: %w", err)
	}
	p.NotifyStore = notifyStore
	notifier := notify.NewNotifier(notifyStore, httpx.NewClient(), h)

	spacesStore, err := spaces.NewStore(
		gormDB.WithContext(ctx),
		notifier,
		spacecommit.NewAuthority(p.HostKey, h),
	)
	if err != nil {
		return nil, fmt.Errorf("setup spaces store: %w", err)
	}
	p.SpacesStore = spacesStore

	bucket := cfg.Bucket
	if bucket == nil {
		bucket, err = blob.OpenBucket(ctx, cfg.BlobBucket)
		if err != nil {
			return nil, fmt.Errorf("open blob bucket: %w", err)
		}
	}
	p.bucket = bucket
	blobStore := spaces.NewBlobStore(bucket)

	permStore := perms.NewStore(gormDB, spacesStore, fgaStore)
	p.PermStore = permStore
	spaceCredential := authn.NewSpaceCredentialAuthMethod(defaultDir)
	validator := authn.NewValidator(
		oauthServer,
		serviceAuth,
		spaceCredential,
		authn.NewDelegationTokenAuthMethod(hiveDir, permStore, p.HostKey),
		permStore,
	)
	p.Validator = validator

	// TODO: use this to validate the space credential in the spaces server
	p.spacesServer = spaces_server.NewServer(spacesStore, validator, p.HostKey, h, blobStore)
	p.notifyServer = notify.NewServer(notifyStore, validator)
	p.simplespaceServer = simplespace.NewServer(
		simplespace.NewStore(gormDB, spacesStore, permStore),
		validator,
	)
	p.relationshipServer = relationship.NewServer(permStore, spacesStore, validator)

	repository, err := repo.NewRepo(gormDB.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("setup repo: %w", err)
	}
	p.Repo = repository

	permissionsStore, err := permissions.NewStore(gormDB, cliqueStore)
	if err != nil {
		return nil, fmt.Errorf("create permission store: %w", err)
	}
	p.Permissions = permissionsStore

	pearStore := pear.NewPear(hiveDir, permissionsStore, repository)
	// Server for org management routes
	orgServer, err := org_server.NewServer(orgStore, validator, cfg.Domain, hiveDir, instanceStore)
	if err != nil {
		return nil, fmt.Errorf("setup org server for domain %q: %w", cfg.Domain, err)
	}
	p.orgServer = orgServer

	p.cliqueServer = clique.NewServer(cliqueStore, validator)
	p.pearServer = pear.NewServer(pearStore, validator, orgStore)

	if !cfg.DisableP2P {
		p2pServer, err := p2p.NewServer(ctx, serviceAuth, pearStore, otelMeter())
		if err != nil {
			return nil, fmt.Errorf("setup p2p server: %w", err)
		}
		p.p2pServer = p2pServer
	}

	pdsForwarding := forwarding.NewPDSForwarding(pdsCredStore, validator, pdsClientFactory, defaultDir)
	p.pdsForwarding = pdsForwarding

	idServer, err := habitat_identity.NewServer(h, validator, orgStore, pdsForwarding, cfg.Domain)
	if err != nil {
		return nil, fmt.Errorf("setup hive server: %w", err)
	}
	p.idServer = idServer

	if !cfg.DisableUI {
		uiHandler, err := webui.New(cfg.UIDevProxy)
		if err != nil {
			return nil, fmt.Errorf("setup embedded UI handler: %w", err)
		}
		p.uiHandler = uiHandler
	}

	hostPublicKey, err := p.HostKey.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get host public key: %w", err)
	}
	p.hostPublicKey = hostPublicKey

	p.Router = mux.NewRouter()
	p.registerMiddleware()
	p.registerRoutes()

	return p, nil
}

// Close releases the resources New acquired: the blob bucket and, when
// enabled, the libp2p host. main defers this; tests register it with
// t.Cleanup.
func (p *Pear) Close() error {
	var errs []error
	if p.bucket != nil {
		if err := p.bucket.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close blob bucket: %w", err))
		}
	}
	if p.p2pServer != nil {
		if err := p.p2pServer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close p2p host: %w", err))
		}
	}
	return errors.Join(errs...)
}

func setupFGA(ctx context.Context, dsn string) (fgastore.Store, error) {
	// Share the main Postgres database for FGA when one is configured; only fall
	// back to a separate SQLite file when the main store is SQLite.
	if db.ParseDialect(dsn) == db.Postgres {
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
	fgaPath := strings.TrimPrefix(dsn, "sqlite://") + ".fga.db"
	fga, err := fgastore.NewSQLite(ctx, fgaPath)
	if err != nil {
		return nil, fmt.Errorf("setup fga sqlite store %q: %w", fgaPath, err)
	}
	return fga, nil
}
