package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/pressly/goose/v3"
	"go.opentelemetry.io/contrib/bridges/otelslog"
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
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/clique"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/sync"

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
	"github.com/lmittmann/tint"
	"github.com/urfave/cli/v3"

	_ "github.com/habitat-network/habitat/cmd/pear/migrations"
)

//go:embed migrations/*.go migrations/*.sql
var embedMigrations embed.FS

func main() {
	flags, mutuallyExclusiveFlags := getFlags()
	cmd := &cli.Command{
		Flags:                  flags,
		MutuallyExclusiveFlags: mutuallyExclusiveFlags,
		Action:                 run,
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		slog.Error("error running command", "err", err)
		os.Exit(1)
	}
}

func run(_ context.Context, cmd *cli.Command) error {
	port := cmd.String(fPort)
	httpsCerts := cmd.String(fHttpsCerts)

	notifyCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Setup OpenTelemetry
	// This needs to happen at the beginning so components use the global logger initialized below
	// by slog.
	otelClose, err := telemetry.SetupOpenTelemetry(notifyCtx)
	defer otelClose(context.Background())
	if err != nil {
		slog.Error("failed setting up open telemetry for metric/trace/log collection", "err", err)
		os.Exit(1)
	}
	slog.Info("successfully set up open telemetry")

	tracer := otel.Tracer("pear/main")
	startupCtx, startupSpan := tracer.Start(notifyCtx, "startup")

	env := utils.GetEnvString("env", "local")
	meter := otel.Meter("habitat-meter", metric.WithInstrumentationAttributes(attribute.KeyValue{
		Key:   "env",
		Value: attribute.StringValue(env),
	}))

	gauge, err := meter.Int64Gauge("habitat.running", metric.WithUnit("item"))
	if err != nil {
		slog.Error("failed to create gauge", "err", err)
	} else {
		gauge.Record(startupCtx, 1)
		defer gauge.Record(context.Background(), 0)
	}

	logHandlers := []slog.Handler{}
	logHandlers = append(logHandlers, otelslog.NewHandler(
		"habitat",
		otelslog.WithLoggerProvider(global.GetLoggerProvider()),
	))
	if cmd.Bool(fDebug) {
		logHandlers = append(logHandlers,
			tint.NewHandler(os.Stdout, &tint.Options{
				AddSource: true,
				Level:     slog.LevelDebug,
			}),
		)
	}
	slog.SetDefault(slog.New(slog.NewMultiHandler(logHandlers...)))

	slog.Info("running with flags", "flags", cmd.FlagNames())

	db := setupDB(cmd)
	fgaStore := setupFGA(startupCtx, cmd)

	credKey, err := encrypt.ParseKey(cmd.String(fPdsCredEncryptKey))
	if err != nil {
		slog.Error("unable to load PDS encryption key", "err", err)
		os.Exit(1)
	}
	pdsCredStore, err := pdscred.NewPDSCredentialStore(db.WithContext(startupCtx), credKey)
	if err != nil {
		slog.Error("unable to setup pds cred store", "err", err)
		os.Exit(1)
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
		slog.Error("unable to setup oauth client", "err", err)
		os.Exit(1)
	}

	mux := mux.NewRouter()

	mux.Use(otelmux.Middleware("habitat-server", otelmux.WithPublicEndpoint()))
	mux.Use(corsMiddleware)
	if cmd.Bool(fDebug) {
		mux.Use(func(next http.Handler) http.Handler {
			return handlers.LoggingHandler(os.Stdout, next)
		})
	}

	hiveDomain := cmd.String(fHiveDomain)
	if hiveDomain == "" {
		hiveDomain = domain
	}

	orgHive, err := hive.NewHive(hiveDomain, domain, db.WithContext(startupCtx))
	if err != nil {
		slog.Error("unable to setup hive (identity service for org)", "err", err)
		os.Exit(1)
	}
	dir := identity.DefaultDirectory()

	pdsClientFactory, err := pdsclient.NewHttpClientFactory(
		pdsCredStore,
		oauthClient,
		dir,
	)
	if err != nil {
		slog.Error("unable to setup PDS client factory", "err", err)
		os.Exit(1)
	}

	oauthSecret, err := encrypt.ParseKey(cmd.String(fOauthServerSecret))
	if err != nil {
		slog.Error("unable to parse oauth server secret for login provider", "err", err)
		os.Exit(1)
	}
	passwordProvider, err := login.NewPasswordProvider(
		db,
		cmd.String(fDomain),
		cmd.String(fFrontendDomain),
		oauthSecret,
		dir,
	)
	if err != nil {
		slog.Error("unable to setup password login provider", "err", err)
		os.Exit(1)
	}
	orgStore, err := org.NewStore(
		db.WithContext(startupCtx),
		orgHive,
		dir,
		domain,
		passwordProvider,
	)
	if err != nil {
		slog.Error("unable to setup org store", "err", err)
		os.Exit(1)
	}

	loginRouter := &org.LoginRouter{
		Pds:      login.NewPDSProvider(oauthClient, pdsCredStore, dir),
		Password: passwordProvider,
		OrgStore: orgStore,
	}
	googleClientID := cmd.String(fGoogleClientID)
	googleClientSecret := cmd.String(fGoogleClientSecret)
	if googleClientID != "" && googleClientSecret != "" {
		googleProvider, err := login.NewGoogleProvider(
			googleClientID,
			googleClientSecret,
			"https://"+domain+"/oauth-callback",
			db.WithContext(startupCtx),
			credKey,
		)
		if err != nil {
			slog.Error("unable to setup google login provider", "err", err)
			os.Exit(1)
		}
		loginRouter.Google = googleProvider
		slog.Info("google login provider enabled")
	}

	oauthServer, err := oauthserver.NewOAuthServer(
		oauthSecret,
		loginRouter,
		dir,
		pdsCredStore,
		db.WithContext(startupCtx),
		meter,
		orgStore,
	)
	if err != nil {
		slog.Error("unable to setup oauth server", "err", err)
		os.Exit(1)
	}

	cliqueStore, err := clique.NewStore(db.WithContext(startupCtx))
	if err != nil {
		slog.Error("unable to setup clique store", "err", err)
		os.Exit(1)
	}

	eventStore, err := events.NewStore(db.WithContext(startupCtx))
	if err != nil {
		slog.Error("unable to setup event store", "err", err)
		os.Exit(1)
	}
	syncServer := sync.NewServer(eventStore)

	spacesStore, err := spaces.NewStore(db.WithContext(startupCtx), fgaStore, eventStore)
	if err != nil {
		slog.Error("unable to setup spaces store", "err", err)
		os.Exit(1)
	}
	spacesServer := spaces.NewServer(
		spacesStore,
		fgaStore,
		oauthServer,
		authn.NewServiceAuthMethod(dir),
		orgStore,
	)

	repo, err := repo.NewRepo(db.WithContext(startupCtx))
	if err != nil {
		slog.Error("unable to setup repo", "err", err)
		os.Exit(1)
	}

	pear, err := setupPear(
		dir,
		repo,
		cliqueStore,
		db.WithContext(startupCtx),
	)
	if err != nil {
		slog.Error("unable to setup pear", "err", err)
		os.Exit(1)
	}

	orgServer, err := org.NewServer(orgStore, oauthServer, pear)
	if err != nil {
		slog.Error("unable to setup org server for domain", "err", err, "domain", domain)
		os.Exit(1)
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
	p2pServer, err := p2p.NewServer(startupCtx, authn.NewServiceAuthMethod(dir), pear, meter)
	if err != nil {
		slog.Error("unable to setup p2p server", "err", err)
		os.Exit(1)
	}

	hiveServer, err := hive.NewServer(orgHive, oauthServer)
	if err != nil {
		slog.Error("unable to setup hive server", "err", err)
		os.Exit(1)
	}

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

	waitlistSvc, err := NewWaitlistService(
		startupCtx,
		os.Getenv("WAITLIST_SHEET_ID"),
		os.Getenv("WAITLIST_SVC_ACCOUNT_CREDS"),
	)
	if err == nil {
		slog.Info("successfully set up waitlist service")
		mux.HandleFunc("/waitlist", waitlistSvc.HandleWaitlistEmailSignup)
	} else {
		slog.Error("unable to set up waitlist service", "err", err)
	}

	mux.HandleFunc("/.well-known/did.json", serveDid(domain))
	mux.HandleFunc("/client-metadata.json", serveClientMetadata(oauthClient))

	mux.HandleFunc("/oauth-callback", oauthServer.HandleCallback)
	mux.HandleFunc("/oauth/authorize", oauthServer.HandleAuthorize)
	mux.HandleFunc("/oauth/token", oauthServer.HandleToken)
	mux.HandleFunc("/xrpc/network.habitat.listConnectedApps", oauthServer.ListConnectedApps)
	mux.HandleFunc("/xrpc/network.habitat.org.loginMember", passwordProvider.HandlePasswordLogin)

	mux.HandleFunc("/xrpc/network.habitat.repo.putRecord", pearServer.PutRecord)
	mux.HandleFunc("/xrpc/network.habitat.repo.getRecord", pearServer.GetRecord)
	mux.HandleFunc("/xrpc/network.habitat.repo.listRecords", pearServer.ListRecords)
	mux.HandleFunc("/xrpc/network.habitat.repo.describeRepo", pearServer.DescribeRepo)
	mux.HandleFunc("/xrpc/com.atproto.repo.describeRepo", pearServer.DescribeRepoPublic)
	mux.HandleFunc("/xrpc/network.habitat.repo.deleteRecord", pearServer.DeleteRecord)
	mux.HandleFunc("/xrpc/network.habitat.repo.createRecord", pearServer.CreateRecord)
	mux.HandleFunc("/xrpc/network.habitat.repo.uploadBlob", pearServer.UploadBlob)
	mux.HandleFunc("/xrpc/network.habitat.repo.getBlob", pearServer.GetBlob)

	mux.HandleFunc("/xrpc/network.habitat.permissions.listPermissions", pearServer.ListPermissions)
	mux.HandleFunc("/xrpc/network.habitat.permissions.addPermission", pearServer.AddPermission)
	mux.HandleFunc(
		"/xrpc/network.habitat.permissions.removePermission",
		pearServer.RemovePermission,
	)

	mux.HandleFunc("/xrpc/network.habitat.clique.createClique", cliqueServer.CreateClique)
	mux.HandleFunc("/xrpc/network.habitat.clique.addMembers", cliqueServer.AddCliqueMembers)
	mux.HandleFunc("/xrpc/network.habitat.clique.removeMembers", cliqueServer.RemoveCliqueMembers)
	mux.HandleFunc("/xrpc/network.habitat.clique.getMembers", cliqueServer.GetCliqueMembers)
	mux.HandleFunc("/xrpc/network.habitat.clique.isMember", cliqueServer.IsCliqueMember)

	mux.HandleFunc("/xrpc/network.habitat.space.createSpace", spacesServer.CreateSpace)
	mux.HandleFunc("/xrpc/network.habitat.space.listSpaces", spacesServer.ListSpaces)
	mux.HandleFunc("/xrpc/network.habitat.space.addMember", spacesServer.AddMember)
	mux.HandleFunc("/xrpc/network.habitat.space.removeMember", spacesServer.RemoveMember)
	mux.HandleFunc("/xrpc/network.habitat.space.getMembers", spacesServer.GetMembers)
	mux.HandleFunc("/xrpc/network.habitat.space.putRecord", spacesServer.PutRecord)
	mux.HandleFunc("/xrpc/network.habitat.space.getRecord", spacesServer.GetRecord)
	mux.HandleFunc("/xrpc/network.habitat.space.listRecords", spacesServer.ListRecords)
	mux.HandleFunc("/xrpc/network.habitat.space.deleteRecord", spacesServer.DeleteRecord)
	mux.HandleFunc("/xrpc/network.habitat.space.deleteSpace", spacesServer.DeleteSpace)
	mux.HandleFunc("/xrpc/network.habitat.space.getRepoOplog", spacesServer.GetRepoOplog)

	mux.HandleFunc("/xrpc/network.habitat.sync.subscribeSpaces", syncServer.HandleSubscribeSpaces)

	pdsForwarding := forwarding.NewPDSForwarding(pdsCredStore, oauthServer, pdsClientFactory, dir)
	mux.PathPrefix("/xrpc/com.atproto.repo.").Handler(pdsForwarding)
	mux.PathPrefix("/xrpc/com.atproto.sync.").Handler(pdsForwarding)

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
					r.Context(),
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
			utils.LogAndHTTPError(
				r.Context(),
				w,
				fmt.Errorf("no atproto_pds or habitat service in DID doc for %s", id.DID),
				"[getServiceAuth dispatch]: no usable service in DID doc",
				http.StatusBadGateway,
			)
		},
	)

	postHogUrl, err := url.Parse("https://us.i.posthog.com")
	if err != nil {
		slog.Error("failed to parse posthog url", "err", err)
		os.Exit(1)
	}
	postHogProxy := httputil.NewSingleHostReverseProxy(postHogUrl)
	defaultDirector := postHogProxy.Director
	postHogProxy.Director = func(req *http.Request) {
		defaultDirector(req)
		req.Host = postHogUrl.Host
	}
	mux.PathPrefix("/posthog").
		Handler(http.StripPrefix("/posthog", postHogProxy))

	mux.PathPrefix("/").HandlerFunc(p2pServer.HandleLibp2p)

	startupSpan.End()

	s := &http.Server{
		Handler: mux,
		Addr:    fmt.Sprintf(":%s", port),
	}

	eg, egCtx := errgroup.WithContext(startupCtx)
	eg.Go(func() error {
		slog.Info("starting sequencer")
		return eventStore.StartSequencer(egCtx)
	})
	eg.Go(func() error {
		slog.Info("starting server", "port", port)
		if httpsCerts == "" {
			return s.ListenAndServe()
		}
		return s.ListenAndServeTLS(
			filepath.Join(httpsCerts, "fullchain.pem"),
			filepath.Join(httpsCerts, "privkey.pem"),
		)
	})

	eg.Go(func() error {
		<-egCtx.Done()
		slog.Info("shutting down p2p server")
		if err := p2pServer.Close(); err != nil {
			slog.Error("error closing p2p host", "err", err)
		}
		slog.Info("shutting down server")
		if err := s.Shutdown(context.Background()); err != nil {
			slog.Error("error shutting down http server", "err", err)
		}
		return nil
	})

	err = eg.Wait()
	if err != nil {
		slog.Error("server shut down returned an error", "err", err)
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
			slog.Error("unable to open postgres db backing pear server")
		}
		slog.Info("connected to postgres database")
	} else {
		dbPath := cmd.String(fDb)
		db, err = gorm.Open(sqlite.Open(dbPath + "?_journal_mode=WAL"))
		if err != nil {
			slog.Error("unable to open sqlite file backing pear server")
		}
		slog.Info("connected to sqlite database", "path", dbPath)
	}
	if err := db.Use(tracing.NewPlugin(tracing.WithoutQueryVariables())); err != nil {
		slog.Error("unable to setup database otel tracing and metrics plugin")
	}
	sqlDb, err := db.DB()
	if err != nil {
		slog.Error("unable to open postgres db backing pear server")
	}
	goose.SetBaseFS(embedMigrations)
	if postgresUrl != "" {
		goose.SetDialect("postgres")
	} else {
		goose.SetDialect("sqlite")
	}
	err = goose.Up(sqlDb, "migrations")
	if err != nil {
		slog.Error("unable to run migrations")
	}
	return db
}

func setupFGA(ctx context.Context, cmd *cli.Command) fgastore.Store {
	postgresUrl := cmd.String(fPgUrl)
	if postgresUrl != "" {
		fga, err := fgastore.NewPostgres(ctx, postgresUrl)
		if err != nil {
			slog.Error("unable to setup fga store with postgres", "err", err)
			os.Exit(1)
		}
		return fga
	}
	// Use a separate SQLite file for FGA to avoid lock conflicts between
	// mattn/go-sqlite3 (used by GORM) and modernc.org/sqlite (used by OpenFGA).
	fgaPath := cmd.String(fDb) + ".fga.db"
	fga, err := fgastore.NewSQLite(ctx, fgaPath)
	if err != nil {
		slog.Error("unable to setup fga sqlite store", "err", err)
		os.Exit(1)
	}
	return fga
}

func setupPear(
	dir identity.Directory,
	repo repo.Repo,
	cliqueStore clique.Store,
	db *gorm.DB,
) (pear.Pear, error) {
	permissions, err := permissions.NewStore(db, cliqueStore)
	if err != nil {
		return nil, fmt.Errorf("failed to create permission store: %w", err)
	}

	return pear.NewPear(dir, permissions, repo), nil
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
