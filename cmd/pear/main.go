package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/contrib/bridges/otelzerolog"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/plugin/opentelemetry/tracing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/gorilla/mux"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/clique"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/inbox"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/node"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/p2p"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/habitat-network/habitat/internal/pear"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"
	"github.com/habitat-network/habitat/internal/server"
	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/habitat-network/habitat/internal/telemetry"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/habitat-network/habitat/internal/xrpcchannel"
	"github.com/urfave/cli/v3"

	_ "github.com/habitat-network/habitat/cmd/pear/migrations"
)

func main() {
	flags, mutuallyExclusiveFlags := getFlags()
	cmd := &cli.Command{
		Flags:                  flags,
		MutuallyExclusiveFlags: mutuallyExclusiveFlags,
		Action:                 run,
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal().Err(err).Msg("error running command")
	}
}

func run(_ context.Context, cmd *cli.Command) error {
	// Parse all CLI arguments and options at the beginning
	port := cmd.String(fPort)
	httpsCerts := cmd.String(fHttpsCerts)

	// Log the parsed flag names (values may be sensitive).
	log.Info().Msgf("running with flags: %s", strings.Join(cmd.FlagNames(), ", "))

	// Setup context with signal handling for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Setup OpenTelemetry
	// This needs to happen at the beginning so components use the global logger initialized below
	// by zerolog.
	otelClose, ok, err := telemetry.SetupOpenTelemetry(ctx)
	// Handle shutdown properly so nothing leaks.
	defer otelClose(context.Background())
	if err != nil {
		log.Fatal().Err(err).Msg("failed setting up open telemetry for metric/trace/log collection")
	}
	if ok {
		log.Info().Msg("successfully set up open telemetry")
	}

	env := utils.GetEnvString("env", "local")
	// Metric that records a single running process (for testing)
	meter := otel.Meter("habitat-meter", metric.WithInstrumentationAttributes(attribute.KeyValue{
		Key:   "env",
		Value: attribute.StringValue(env),
	}))

	gauge, err := meter.Int64Gauge("habitat.running", metric.WithUnit("item"))
	if err != nil {
		log.Err(err)
	} else {
		gauge.Record(ctx, 1)
		// Set to zero when the task goes away
		defer gauge.Record(context.Background(), 0)
	}

	// Setup the zerolog logger
	hook := otelzerolog.NewHook(
		"habitat", /* otel service name */
		otelzerolog.WithLoggerProvider(global.GetLoggerProvider()),
	)

	// Need to set log.Logger so globally anything initialized after here uses the global zerolog Logger
	// which is now hooked up to open telemetry.
	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger().Hook(hook)

	// Setup components
	db := setupDB(cmd)

	var fgaStore fgastore.Store
	fgaPgUrl := cmd.String(fFgaPgUrl)
	if fgaPgUrl != "" {
		fga, err := fgastore.NewPostgres(ctx, fgaPgUrl)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to setup fga store with postgres")
		}
		fgaStore = fga
	} else {
		fga, err := fgastore.NewInMemory(ctx)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to setup in-memory fga store")
		}
		fgaStore = fga
	}

	// Load encryption key for PDS credentials
	credKey, err := encrypt.ParseKey(cmd.String(fPdsCredEncryptKey))
	if err != nil {
		log.Fatal().Err(err).Msg("unable to load PDS encryption key")
	}
	pdsCredStore, err := pdscred.NewPDSCredentialStore(db, credKey)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup pds cred store")
	}

	domain := cmd.String(fDomain)
	oauthClient, err := pdsclient.NewPdsOAuthClient(
		"https://"+domain+"/client-metadata.json", /*clientId*/
		"https://"+domain,                         /*clientUri*/
		"https://"+domain+"/oauth-callback",       /*redirectUri*/
		cmd.String(fOauthClientSecret),
		meter,
	)
	if err != nil {
		log.Fatal().Err(err).Msgf("unable to setup oauth client")
	}

	// Create error group for managing goroutines
	eg, egCtx := errgroup.WithContext(ctx)
	mux := mux.NewRouter()

	// Order of middlewares = order of "Use" called
	// https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux
	mux.Use(otelmux.Middleware("habitat-server"))
	mux.Use(corsMiddleware)

	hiveDomain := cmd.String(fHiveDomain)
	if hiveDomain == "" {
		hiveDomain = domain
	}

	// hive is the identity minting service for orgs.
	orgHive, err := hive.NewHive(hiveDomain, domain, db)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup hive (identity service for org)")
	}
	dir := identity.DefaultDirectory()

	// orgStore manages all orgs on this instance (managed orgs + everyone org for external DIDs)
	orgStore, err := org.NewStore(db, orgHive, dir, domain)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup org store")
	}

	pdsClientFactory, err := pdsclient.NewHttpClientFactory(
		pdsCredStore,
		oauthClient,
		dir,
	)
	if err != nil {
		log.Fatal().Err(err).Msgf("unable to setup PDS client factory")
	}

	node := setupNode(cmd, pdsClientFactory, dir)

	oauthSecret, err := encrypt.ParseKey(cmd.String(fOauthServerSecret))
	if err != nil {
		log.Fatal().Err(err).Msg("unable to parse oauth server secret for login provider")
	}
	orgLoginProvider := org.NewLoginProvider(
		orgStore,
		cmd.String(fDomain),
		cmd.String(fFrontendDomain),
		oauthSecret,
		dir,
	)

	providers := []login.Provider{
		login.NewPDSProvider(oauthClient, pdsCredStore, dir),
		orgLoginProvider,
	}
	googleClientID := cmd.String(fGoogleClientID)
	googleClientSecret := cmd.String(fGoogleClientSecret)
	if googleClientID != "" && googleClientSecret != "" {
		googleProvider, err := login.NewGoogleProvider(
			googleClientID,
			googleClientSecret,
			"https://"+domain+"/oauth-callback",
			db,
			credKey,
		)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to setup google login provider")
		}
		providers = append(providers, googleProvider)
		log.Info().Msg("google login provider enabled")
	}
	loginRouter := login.NewRouter(providers...)

	oauthServer, err := oauthserver.NewOAuthServer(
		oauthSecret,
		loginRouter,
		node,
		dir,
		pdsCredStore,
		db,
		meter,
		orgStore,
	)
	if err != nil {
		log.Fatal().Err(err).Msgf("unable to setup oauth server")
	}

	cliqueStore, err := clique.NewStore(db)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup clique store")
	}

	spacesStore, err := spaces.NewStore(db, fgaStore)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup spaces store")
	}
	spacesServer := spaces.NewServer(spacesStore, oauthServer, authn.NewServiceAuthMethod(dir))

	cdc := repo.NewChangeEmitter(ctx, repo.DefaultChangeBufferSize)
	repo, err := repo.NewRepo(cdc, db)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup repo")
	}

	pear, err := setupPear(ctx, cmd, dir, repo, node, cliqueStore, db)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup pear")
	}

	// Server for org management routes
	orgServer, err := org.NewServer(orgStore, oauthServer, pear)
	if err != nil {
		log.Fatal().Err(err).Msgf("unable to setup org server for domain: %s", domain)
	}
	mux.HandleFunc("/xrpc/network.habitat.org.getAdmins", orgServer.GetAdmins)
	mux.HandleFunc("/xrpc/network.habitat.org.getMembers", orgServer.GetMembers)
	mux.HandleFunc("/xrpc/network.habitat.org.addAdmin", orgServer.AddAdmin)
	mux.HandleFunc("/xrpc/network.habitat.org.removeAdmin", orgServer.RemoveAdmin)
	mux.HandleFunc("/xrpc/network.habitat.org.removeMembers", orgServer.RemoveMembers)
	mux.HandleFunc("/xrpc/network.habitat.org.downgradeAdmin", orgServer.DowngradeAdmin)
	mux.HandleFunc("/xrpc/network.habitat.org.issueInviteToken", orgServer.IssueInviteToken)
	mux.HandleFunc("/xrpc/network.habitat.org.mintMemberIdentity", orgServer.MintMemberIdentity)
	mux.HandleFunc("/xrpc/network.habitat.org.create", orgServer.CreateOrg)

	cliqueServer := clique.NewServer(cliqueStore, oauthServer, authn.NewServiceAuthMethod(dir))

	pearServer := server.NewServer(
		dir,
		pear,
		oauthServer,
		authn.NewServiceAuthMethod(dir),
		orgStore,
	)
	p2pServer, err := p2p.NewServer(authn.NewServiceAuthMethod(dir), pear, meter)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup p2p server")
	}

	hiveServer, err := hive.NewServer(orgHive, oauthServer)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup hive server")
	}

	// hive server routes
	mux.Host("{opaqueID:.+}." + hiveDomain).
		Path("/.well-known/did.json").
		HandlerFunc(hiveServer.ServeDIDDoc)
	mux.Host("{handle:.+}." + hiveDomain).
		Path("/.well-known/atproto-did").
		HandlerFunc(hiveServer.ServeHandle)
	mux.Headers(hive.HabitatHostHeader, "").
		Path("/.well-known/did.json").
		HandlerFunc(hiveServer.ServeDIDDoc)
	mux.Headers(hive.HabitatHostHeader, "").
		Path("/.well-known/atproto-did").
		HandlerFunc(hiveServer.ServeHandle)

	// handle waitlist signups
	// TODO: this should be moved to a separate server; no need to run it for orgs
	waitlistSvc, err := NewWaitlistService(
		egCtx,
		os.Getenv("WAITLIST_SHEET_ID"),
		os.Getenv("WAITLIST_SVC_ACCOUNT_CREDS"),
	)
	if err == nil {
		log.Info().Msgf("successfully set up waitlist service")
		mux.HandleFunc("/waitlist", waitlistSvc.HandleWaitlistEmailSignup)
	} else {
		// Not a fatal error: log and move on
		log.Err(err).Msgf("unable to set up waitlist service")
	}

	// TODO: enable this when jetstream has auth on it
	/*
		consumer, err := changeEmitter.Consume()
		if err != nil {
			log.Fatal().Err(err).Msg("unable to setup change emitter consumer for jetstream service")
		}
		jss := jetstream.NewServer(egCtx, consumer)
		mux.HandleFunc("/jetstream", jss.HandleSubscribe)
	*/

	// always public routes
	mux.HandleFunc("/.well-known/did.json", serveDid(domain))
	mux.HandleFunc("/client-metadata.json", serveClientMetadata(oauthClient))

	// TODO: who is allowed to call the oauth handlers in an org?
	mux.HandleFunc("/oauth-callback", oauthServer.HandleCallback)
	mux.HandleFunc("/oauth/authorize", oauthServer.HandleAuthorize)
	mux.HandleFunc("/oauth/token", oauthServer.HandleToken)
	mux.HandleFunc("/xrpc/network.habitat.listConnectedApps", oauthServer.ListConnectedApps)
	mux.HandleFunc("/xrpc/network.habitat.org.loginMember", orgLoginProvider.HandlePasswordLogin)

	// pear routes
	// repo
	mux.HandleFunc("/xrpc/network.habitat.repo.putRecord", pearServer.PutRecord)
	mux.HandleFunc("/xrpc/network.habitat.repo.getRecord", pearServer.GetRecord)
	mux.HandleFunc("/xrpc/network.habitat.repo.listRecords", pearServer.ListRecords)
	mux.HandleFunc("/xrpc/network.habitat.repo.describeRepo", pearServer.DescribeRepo)
	mux.HandleFunc("/xrpc/com.atproto.repo.describeRepo", pearServer.DescribeRepoPublic)
	mux.HandleFunc("/xrpc/network.habitat.repo.deleteRecord", pearServer.DeleteRecord)
	mux.HandleFunc("/xrpc/network.habitat.repo.createRecord", pearServer.CreateRecord)
	mux.HandleFunc("/xrpc/network.habitat.repo.uploadBlob", pearServer.UploadBlob)
	mux.HandleFunc("/xrpc/network.habitat.repo.getBlob", pearServer.GetBlob)

	// permissions
	mux.HandleFunc("/xrpc/network.habitat.permissions.listPermissions", pearServer.ListPermissions)
	mux.HandleFunc("/xrpc/network.habitat.permissions.addPermission", pearServer.AddPermission)
	mux.HandleFunc(
		"/xrpc/network.habitat.permissions.removePermission",
		pearServer.RemovePermission,
	)

	// cliques
	mux.HandleFunc("/xrpc/network.habitat.clique.createClique", cliqueServer.CreateClique)
	mux.HandleFunc("/xrpc/network.habitat.clique.addMembers", cliqueServer.AddCliqueMembers)
	mux.HandleFunc("/xrpc/network.habitat.clique.removeMembers", cliqueServer.RemoveCliqueMembers)
	mux.HandleFunc("/xrpc/network.habitat.clique.getMembers", cliqueServer.GetCliqueMembers)
	mux.HandleFunc("/xrpc/network.habitat.clique.isMember", cliqueServer.IsCliqueMember)

	// spaces
	mux.HandleFunc("/xrpc/network.habitat.space.createSpace", spacesServer.CreateSpace)
	mux.HandleFunc("/xrpc/network.habitat.space.listSpaces", spacesServer.ListSpaces)
	mux.HandleFunc("/xrpc/network.habitat.space.addMember", spacesServer.AddMember)
	mux.HandleFunc("/xrpc/network.habitat.space.removeMember", spacesServer.RemoveMember)
	mux.HandleFunc("/xrpc/network.habitat.space.getMembers", spacesServer.GetMembers)
	mux.HandleFunc("/xrpc/network.habitat.space.putRecord", spacesServer.PutRecord)
	mux.HandleFunc("/xrpc/network.habitat.space.getRecord", spacesServer.GetRecord)
	mux.HandleFunc("/xrpc/network.habitat.space.listRecords", spacesServer.ListRecords)
	mux.HandleFunc("/xrpc/network.habitat.space.deleteRecord", spacesServer.DeleteRecord)

	pdsForwarding := forwarding.NewPDSForwarding(pdsCredStore, oauthServer, pdsClientFactory, dir)
	// Only forward specific routes that we know we handle correctly; for now.
	mux.PathPrefix("/xrpc/com.atproto.repo.").Handler(pdsForwarding)
	mux.PathPrefix("/xrpc/com.atproto.sync.").Handler(pdsForwarding)

	// getServiceAuth needs to be dispatched per-caller: bridged users whose
	// DID doc lists an #atproto_pds service must still have their request
	// forwarded to that PDS (which holds their signing key). For habitat-native
	// users whose doc only has #habitat, habitat itself holds the signing key
	// and mints the JWT locally via hiveServer. Prefer atproto_pds when both
	// services are present.
	mux.HandleFunc(
		"/xrpc/com.atproto.server.getServiceAuth",
		func(w http.ResponseWriter, r *http.Request) {
			callerDID, ok := oauthServer.Validate(w, r)
			if !ok {
				return
			}
			id, err := dir.LookupDID(r.Context(), callerDID)
			if err != nil {
				utils.LogAndHTTPError(
					w,
					err,
					"[getServiceAuth dispatch]: looking up caller DID",
					http.StatusBadGateway,
				)
				return
			}
			if _, ok := id.Services["atproto_pds"]; ok {
				pdsForwarding.ServeHTTP(w, r)
				return
			}
			if _, ok := id.Services["habitat"]; ok {
				hiveServer.GetServiceAuth(w, r)
				return
			}
			utils.LogAndHTTPError(w,
				fmt.Errorf("no atproto_pds or habitat service in DID doc for %s", id.DID),
				"[getServiceAuth dispatch]: no usable service in DID doc",
				http.StatusBadGateway,
			)
		},
	)

	postHogUrl, err := url.Parse("https://us.i.posthog.com")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to parse posthog url")
	}
	postHogProxy := httputil.NewSingleHostReverseProxy(postHogUrl)
	defaultDirector := postHogProxy.Director
	postHogProxy.Director = func(req *http.Request) {
		defaultDirector(req)
		req.Host = postHogUrl.Host
	}
	mux.PathPrefix("/posthog").
		Handler(http.StripPrefix("/posthog", postHogProxy))

	// TODO: should we put this behind /p2p instead of / ?
	mux.PathPrefix("/").HandlerFunc(p2pServer.HandleLibp2p)

	s := &http.Server{
		Handler: mux,
		Addr:    fmt.Sprintf(":%s", port),
	}

	// Start the HTTP server in a goroutine
	eg.Go(func() error {
		log.Info().Msgf("starting server on port :%s", port)
		if httpsCerts == "" {
			return s.ListenAndServe()
		}
		return s.ListenAndServeTLS(
			filepath.Join(httpsCerts, "fullchain.pem"),
			filepath.Join(httpsCerts, "privkey.pem"),
		)
	})

	// Gracefully shutdown server when context is cancelled
	eg.Go(func() error {
		<-egCtx.Done()
		log.Info().Msg("shutting down p2p server")
		if err := p2pServer.Close(); err != nil {
			log.Error().Err(err).Msg("error closing p2p host")
		}
		log.Info().Msg("shutting down server")
		if err := s.Shutdown(context.Background()); err != nil {
			log.Error().Err(err).Msg("error shutting down http server")
		}
		return nil
	})

	// Wait for all goroutines to finish
	err = eg.Wait()
	if err != nil {
		log.Err(err).Msgf("server shut down returned an error")
	}
	return err
}

func serveDid(domain string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		template := `{
  "id": "did:web:%s",
  "@context": [
    "https://www.w3.org/ns/did/v1",
    "https://w3id.org/security/multikey/v1", 
    "https://w3id.org/security/suites/secp256k1-2019/v1"
  ],
  "service": [
    {
      "id": "#habitat",
      "serviceEndpoint": "https://%s",
      "type": "HabitatServer"
    }
  ]
}`
		_, err := fmt.Fprintf(w, template, domain, domain)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func serveClientMetadata(oauthClient pdsclient.PdsOAuthClient) http.HandlerFunc {
	metadata := oauthClient.ClientMetadata()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(metadata); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func setupDB(cmd *cli.Command) *gorm.DB {
	var db *gorm.DB
	var err error

	postgresUrl := cmd.String(fPgUrl)
	if postgresUrl != "" {
		db, err = gorm.Open(postgres.Open(postgresUrl), &gorm.Config{})
		if err != nil {
			log.Fatal().Err(err).Msg("unable to open postgres db backing pear server")
		}
		log.Info().Msg("connected to postgres database")
	} else {
		dbPath := cmd.String(fDb)
		db, err = gorm.Open(sqlite.Open(dbPath))
		if err != nil {
			log.Fatal().Err(err).Msg("unable to open sqlite file backing pear server")
		}
		log.Info().Str("path", dbPath).Msg("connected to sqlite database")
	}
	if err := db.Use(tracing.NewPlugin(tracing.WithoutQueryVariables())); err != nil {
		log.Fatal().Err(err).Msg("unable to setup database otel tracing and metrics plugin")
	}
	sqlDb, err := db.DB()
	if err != nil {
		log.Fatal().Err(err).Msg("unable to open postgres db backing pear server")
	}
	if postgresUrl != "" {
		goose.SetDialect("postgres")
	} else {
		goose.SetDialect("sqlite")
	}
	err = goose.Up(sqlDb, "migrations")
	if err != nil {
		log.Fatal().Err(err).Msg("unable to run migrations")
	}
	return db
}

func setupNode(
	cmd *cli.Command,
	clientFactory pdsclient.HttpClientFactory,
	dir identity.Directory,
) node.Node {
	serviceName := cmd.String(fServiceName)
	domain := cmd.String(fDomain)
	serviceEndpoint := "https://" + domain
	xrpcCh := xrpcchannel.NewServiceProxyXrpcChannel(serviceName, clientFactory, dir)
	return node.New(
		serviceName,
		serviceEndpoint,
		dir,
		xrpcCh,
		// add self fallback just for medium term public demos
		node.WithSelfFallback(),
	)
}

func setupPear(
	ctx context.Context,
	cmd *cli.Command,
	dir identity.Directory,
	repo repo.Repo,
	node node.Node,
	cliqueStore clique.Store,
	db *gorm.DB,
) (pear.Pear, error) {
	permissions, err := permissions.NewStore(db, cliqueStore)
	if err != nil {
		return nil, fmt.Errorf("failed to create permission store: %w", err)
	}

	inbox, err := inbox.New(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create inbox: %w", err)
	}

	return pear.NewPear(node, dir, permissions, repo, inbox), nil
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().
			Set("Access-Control-Allow-Headers", "Content-Type, Authorization, habitat-auth-method, User-Agent, atproto-accept-labelers, atproto-proxy ")
		w.Header().Set("Access-Control-Max-Age", "86400") // Cache preflight for 24 hours

		// Handle preflight OPTIONS request
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
