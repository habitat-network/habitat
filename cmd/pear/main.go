package main

import (
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/alexedwards/argon2id"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/clique"
	habitatdb "github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/events"
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
	"github.com/habitat-network/habitat/internal/sync"
	"go.opentelemetry.io/otel/trace"

	"github.com/habitat-network/habitat/internal/log"
	"github.com/habitat-network/habitat/internal/p2p"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/habitat-network/habitat/internal/pear"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/relationship"
	"github.com/habitat-network/habitat/internal/repo"
	"github.com/habitat-network/habitat/internal/server"
	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/habitat-network/habitat/internal/telemetry"
	"github.com/habitat-network/habitat/internal/webui"
	"github.com/urfave/cli/v3"

	_ "github.com/habitat-network/habitat/cmd/pear/migrations"
)

//go:embed migrations/*.go migrations/*.sql
var embedMigrations embed.FS

func main() {
	cmd := &cli.Command{
		Flags:  getFlags(),
		Action: run,
	}
	ctx := context.Background()
	if err := cmd.Run(ctx, os.Args); err != nil {
		slog.ErrorContext(ctx, "error running command", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	port := cmd.String(fPort)
	httpsCerts := cmd.String(fHttpsCerts)

	notifyCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Setup OpenTelemetry
	// This needs to happen at the beginning so components use the global logger initialized below
	// by slog.
	otelClose, err := telemetry.SetupOpenTelemetry(notifyCtx, "pear")
	defer func() { _ = otelClose(context.Background()) }()
	if err != nil {
		return fmt.Errorf("setup open telemetry for metric/trace/log collection: %w", err)
	}
	slog.InfoContext(notifyCtx, "successfully set up open telemetry")

	tracer := otel.Tracer("pear/main")
	startupCtx, startupSpan := tracer.Start(notifyCtx, "startup")

	meter := otel.Meter("habitat-meter")

	gauge, err := meter.Int64Gauge("habitat.running", metric.WithUnit("item"))
	if err != nil {
		slog.ErrorContext(startupCtx, "failed to create gauge", "err", err)
	} else {
		gauge.Record(startupCtx, 1)
		defer gauge.Record(context.Background(), 0)
	}

	slog.InfoContext(startupCtx, "running with flags", "flags", cmd.FlagNames())

	db, err := habitatdb.New(cmd.String(fDB), habitatdb.WithMigrations(embedMigrations))
	if err != nil {
		return fmt.Errorf("setup database: %w", err)
	}
	fgaStore, err := setupFGA(startupCtx, cmd)
	if err != nil {
		return fmt.Errorf("setup fga store: %w", err)
	}

	oauthSecret, err := encrypt.ParseKey(cmd.String(fOauthServerSecret))
	if err != nil {
		return fmt.Errorf("parse oauth server secret for login provider: %w", err)
	}

	passwordHash, err := setupInstanceAdminPassword(startupCtx, cmd)
	if err != nil {
		return fmt.Errorf("setup instance admin password: %w", err)
	}
	// Reuse the oauth server secret
	instanceAdminStore := instance.NewStore(
		db,
		oauthSecret,
		fDomain,
		passwordHash,
	)

	instanceAdminServer := instance.NewServer(instanceAdminStore, "habitat.network")

	credKey, err := encrypt.ParseKey(cmd.String(fPdsCredEncryptKey))
	if err != nil {
		return fmt.Errorf("load PDS encryption key: %w", err)
	}
	pdsCredStore, err := pdscred.NewPDSCredentialStore(db, credKey)
	if err != nil {
		return fmt.Errorf("setup pds cred store: %w", err)
	}

	domain := cmd.String(fDomain)
	var clientURI string
	if cmd.String(fPdsOauthClientUri) != "" {
		clientURI = "https://" + cmd.String(fPdsOauthClientUri)
	}
	if clientURI == "" {
		clientURI = "https://" + domain
	}
	oauthClient, err := pdsclient.NewPdsOAuthClient(
		clientURI+"/client-metadata.json",
		clientURI,
		"https://"+domain+"/oauth-callback",
		cmd.String(fOauthClientSecret),
		meter,
	)
	if err != nil {
		return fmt.Errorf("setup oauth client: %w", err)
	}

	mux := mux.NewRouter()

	mux.Use(otelmux.Middleware("habitat-server", otelmux.WithPublicEndpoint()))
	mux.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			span := trace.SpanFromContext(r.Context())
			span.SetAttributes(attribute.String("http.request.header.referer", r.Referer()))
			next.ServeHTTP(w, r)
		})
	})
	mux.Use(handlers.CORS(
		handlers.AllowedOrigins([]string{"*"}),
		handlers.AllowedMethods([]string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}),
		handlers.AllowedHeaders([]string{
			"Content-Type",
			"Authorization",
			"habitat-auth-method",
			"User-Agent",
			"atproto-accept-labelers",
			"atproto-proxy",
			"DPoP",
		}),
		handlers.MaxAge(86400),
		handlers.ExposedHeaders([]string{"DPoP-Nonce"}),
	))
	if cmd.Bool(fDebug) {
		mux.Use(func(next http.Handler) http.Handler {
			return handlers.LoggingHandler(os.Stdout, next)
		})
	}

	// Canonical liveness endpoint. Registered before any auth-gated routes so
	// it stays reachable without credentials; used by deploy healthchecks and
	// the startup smoke test.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	hiveDomain := cmd.String(fHiveDomain)
	if hiveDomain == "" {
		hiveDomain = domain
	}

	hive := hive.NewHive(hiveDomain, domain, db)
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
		return fmt.Errorf("setup PDS client factory: %w", err)
	}

	passwordProvider := login.NewPasswordProvider(
		db,
		cmd.String(fDomain),
		oauthSecret,
		hiveDir,
	)
	orgStore := org.NewStore(
		db,
		hive,
		hiveDir,
		domain,
		passwordProvider,
		fgaStore,
	)

	loginRouter := &org.LoginRouter{
		Pds:      login.NewPDSProvider(oauthClient, pdsCredStore, defaultDir),
		Password: passwordProvider,
		OrgStore: orgStore,
	}
	googleClientID := cmd.String(fGoogleClientID)
	googleClientSecret := cmd.String(fGoogleClientSecret)
	var googleLoginProvider habitatdb.MigratableStore
	if googleClientID != "" && googleClientSecret != "" {
		glp, err := login.NewGoogleProvider(
			googleClientID,
			googleClientSecret,
			"https://"+domain+"/oauth-callback",
			db,
			credKey,
		)
		if err != nil {
			return fmt.Errorf("setup google login provider: %w", err)
		}
		loginRouter.Google = glp
		googleLoginProvider = glp
		slog.InfoContext(startupCtx, "google login provider enabled")
	}

	oauthServer, err := oauthserver.NewOAuthServer(
		oauthSecret,
		loginRouter,
		// OAuth server needs privileged access to lookup hive-hosted identities
		hiveDir,
		db,
		meter,
		orgStore,
		"https://"+domain,
		oauthserver.NewJWTBearerStore(
			cmd.StringSlice(fBuiltinApps)...,
		),
	)
	if err != nil {
		return fmt.Errorf("setup oauth server: %w", err)
	}

	// Implement service proxying https://atproto.com/specs/xrpc#service-proxying
	mux.Use(forwarding.NewServiceProxy(oauthServer, hive, hiveDir, pdsClientFactory))

	cliqueStore := clique.NewStore(db)
	eventStore := events.NewStore(db)
	syncServer := sync.NewServer(eventStore)
	notifyStore := notify.NewStore(db)
	notifier := notify.NewNotifier(notifyStore, http.DefaultClient, hive)

	spacesStore := spaces.NewStore(db, fgaStore, eventStore, notifier)
	serviceAuth := authn.NewServiceAuthMethod(defaultDir, fmt.Sprintf("did:web:%s#habitat", domain))

	// Habitat's single host signing key signs permissioned-repo commits for repo
	// owners on external PDSes (habitat-managed owners sign with their own hive
	// key instead). Optional: if unset, host-signed commits are omitted.
	hostKey, err := atcrypto.ParsePrivateMultibase(cmd.String(fSpaceSigningKey))
	if err != nil {
		return fmt.Errorf("parse space-host signing key: %w", err)
	}
	spacesServer := spaces.NewServer(
		spacesStore,
		fgaStore,
		oauthServer,
		serviceAuth,
		authn.NewDelegationTokenAuthMethod(hiveDir, fgaStore),
		orgStore,
		hostKey,
		hive,
	)
	notifyServer := notify.NewServer(notifyStore, authn.NewSpaceCredentialAuthMethod(defaultDir))

	relationshipStore := relationship.NewStore(db, spacesStore, fgaStore)
	relationshipServer := relationship.NewServer(
		relationshipStore,
		fgaStore,
		oauthServer,
		serviceAuth,
	)

	repo := repo.NewRepo(db)
	permissions := permissions.NewStore(db, cliqueStore)

	// AutoMigrate all database models at once
	if err := habitatdb.AutoMigrate(startupCtx, db,
		instanceAdminStore,
		pdsCredStore,
		hive,
		passwordProvider,
		orgStore,
		googleLoginProvider,
		oauthServer,
		cliqueStore,
		eventStore,
		notifyStore,
		spacesStore,
		repo,
		permissions,
	); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	pear := pear.NewPear(hiveDir, permissions, repo)
	// Server for org management routes
	orgServer, err := org_server.NewServer(
		orgStore,
		oauthServer,
		pear,
		domain,
		hiveDir,
		instanceAdminStore,
	)
	if err != nil {
		return fmt.Errorf("setup org server for domain %q: %w", domain, err)
	}
	mux.HandleFunc("/xrpc/network.habitat.org.getMetadata", orgServer.GetMetadata)
	mux.HandleFunc("/xrpc/network.habitat.org.getAdmins", orgServer.GetAdmins)
	mux.HandleFunc("/xrpc/network.habitat.org.getMembers", orgServer.GetMembers)
	mux.HandleFunc("/xrpc/network.habitat.org.addAdmin", orgServer.AddAdmin)
	mux.HandleFunc("/xrpc/network.habitat.org.removeAdmin", orgServer.RemoveAdmin)
	mux.HandleFunc("/xrpc/network.habitat.org.removeMembers", orgServer.RemoveMembers)
	mux.HandleFunc("/xrpc/network.habitat.org.downgradeAdmin", orgServer.DowngradeAdmin)
	mux.HandleFunc("/xrpc/network.habitat.org.issueInviteToken", orgServer.IssueInviteToken)
	mux.HandleFunc("/xrpc/network.habitat.org.mintMemberIdentity", orgServer.MintMemberIdentity)
	mux.HandleFunc("/xrpc/network.habitat.org.create", orgServer.CreateOrg)

	cliqueServer := clique.NewServer(cliqueStore, oauthServer, serviceAuth)
	pearServer := server.NewServer(
		pear,
		oauthServer,
		serviceAuth,
		orgStore,
	)
	p2pServer, err := p2p.NewServer(startupCtx, serviceAuth, pear, meter)
	if err != nil {
		return fmt.Errorf("setup p2p server: %w", err)
	}
	pdsForwarding := forwarding.NewPDSForwarding(
		pdsCredStore,
		oauthServer,
		pdsClientFactory,
		defaultDir,
	)

	idServer, err := habitat_identity.NewServer(hive, oauthServer, orgStore, pdsForwarding)
	if err != nil {
		return fmt.Errorf("setup hive server: %w", err)
	}

	mux.Host("{opaqueID:.+}." + hiveDomain).
		Path("/.well-known/did.json").
		HandlerFunc(idServer.ServeDIDDoc)
	mux.Host("{handle:.+}." + hiveDomain).
		Path("/.well-known/atproto-did").
		HandlerFunc(idServer.ServeHandle)
	mux.Headers(habitat_identity.HabitatHostHeader, "").
		Path("/.well-known/did.json").
		HandlerFunc(idServer.ServeDIDDoc)
	mux.Headers(habitat_identity.HabitatHostHeader, "").
		Path("/.well-known/atproto-did").
		HandlerFunc(idServer.ServeHandle)

	waitlistSvc, err := NewWaitlistService(
		startupCtx,
		os.Getenv("WAITLIST_SHEET_ID"),
		os.Getenv("WAITLIST_SVC_ACCOUNT_CREDS"),
	)
	if err == nil {
		slog.InfoContext(startupCtx, "successfully set up waitlist service")
		mux.HandleFunc("/waitlist", waitlistSvc.HandleWaitlistEmailSignup)
	} else {
		slog.ErrorContext(startupCtx, "unable to set up waitlist service", "err", err)
	}

	mux.HandleFunc("/admin/login", instanceAdminServer.ServeLoginPage).Methods("GET")
	mux.HandleFunc("/admin/login", instanceAdminServer.HandleLogin).Methods("POST")
	mux.HandleFunc("/admin/logout", instanceAdminServer.HandleLogout).Methods("POST")
	mux.HandleFunc("/admin", instanceAdminServer.ServeAdminHome).Methods("GET")
	mux.HandleFunc("/admin/config", instanceAdminServer.ServeConfig).Methods("GET")
	mux.HandleFunc("/xrpc/network.habitat.admin.getSettings", instanceAdminServer.GetSettings)
	mux.HandleFunc("/xrpc/network.habitat.admin.updateSettings", instanceAdminServer.UpdateSettings)
	mux.HandleFunc("/xrpc/network.habitat.admin.issueInvite", instanceAdminServer.IssueInvite)
	mux.HandleFunc(
		"/xrpc/network.habitat.instance.describeInstance",
		instanceAdminServer.DescribeInstance,
	)

	hostPublicKey, err := hostKey.PublicKey()
	if err != nil {
		return fmt.Errorf("failed to get host public key: %w", err)
	}
	mux.HandleFunc("/.well-known/did.json", serveDid(domain, hostPublicKey))
	mux.HandleFunc("/client-metadata.json", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(r.Context(), w, oauthClient.ClientMetadata())
	})

	mux.HandleFunc(
		"/.well-known/oauth-authorization-server",
		oauthServer.HandleAuthServerMetadata,
	)
	mux.HandleFunc(
		"/.well-known/oauth-protected-resource",
		oauthServer.HandleProtectedResourceMetadata,
	)

	mux.HandleFunc("/oauth-callback", oauthServer.HandleCallback)
	mux.HandleFunc("/oauth/authorize", oauthServer.HandleAuthorize)
	mux.HandleFunc("/oauth/par", oauthServer.HandlePAR)
	mux.HandleFunc("/oauth/token", oauthServer.HandleToken)
	mux.HandleFunc("/xrpc/network.habitat.listConnectedApps", oauthServer.ListConnectedApps)
	mux.HandleFunc("/xrpc/network.habitat.org.loginMember", passwordProvider.HandlePasswordLogin)

	mux.HandleFunc("/xrpc/network.habitat.repo.putRecord", pearServer.PutRecord)
	mux.HandleFunc("/xrpc/network.habitat.repo.getRecord", pearServer.GetRecord)
	mux.HandleFunc("/xrpc/network.habitat.repo.listRecords", pearServer.ListRecords)
	mux.HandleFunc("/xrpc/network.habitat.repo.describeRepo", pearServer.DescribeRepo)
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
	mux.HandleFunc("/xrpc/network.habitat.space.listRepos", spacesServer.ListRepos)
	mux.HandleFunc("/xrpc/network.habitat.space.putRecord", spacesServer.PutRecord)
	mux.HandleFunc("/xrpc/network.habitat.space.getRecord", spacesServer.GetRecord)
	mux.HandleFunc("/xrpc/network.habitat.space.listRecords", spacesServer.ListRecords)
	mux.HandleFunc("/xrpc/network.habitat.space.deleteRecord", spacesServer.DeleteRecord)
	mux.HandleFunc("/xrpc/network.habitat.space.deleteSpace", spacesServer.DeleteSpace)
	mux.HandleFunc("/xrpc/network.habitat.space.listRepoOps", spacesServer.ListRepoOps)
	mux.HandleFunc("/xrpc/com.atproto.space.getRepo", spacesServer.GetRepo)
	mux.HandleFunc("/xrpc/network.habitat.space.registerNotify", notifyServer.RegisterNotify)

	mux.HandleFunc(
		"/xrpc/network.habitat.relationship.writeTuple",
		relationshipServer.WriteTuple,
	)
	mux.HandleFunc(
		"/xrpc/network.habitat.relationship.deleteTuple",
		relationshipServer.DeleteTuple,
	)
	mux.HandleFunc("/xrpc/network.habitat.relationship.listTuples", relationshipServer.ListTuples)
	mux.HandleFunc("/xrpc/network.habitat.relationship.check", relationshipServer.Check)
	mux.HandleFunc(
		"/xrpc/network.habitat.relationship.listSubjects",
		relationshipServer.ListSubjects,
	)
	mux.HandleFunc(
		"/xrpc/network.habitat.relationship.listObjects",
		relationshipServer.ListObjects,
	)
	mux.HandleFunc("/xrpc/network.habitat.sync.subscribeSpaces", syncServer.HandleSubscribeSpaces)

	mux.PathPrefix("/xrpc/com.atproto.repo.").Handler(pdsForwarding)
	mux.PathPrefix("/xrpc/com.atproto.sync.").Handler(pdsForwarding)

	mux.HandleFunc("/xrpc/com.atproto.server.getServiceAuth", idServer.GetServiceAuth)

	uiHandler, err := webui.New(cmd.String(fUiDevProxy))
	if err != nil {
		return fmt.Errorf("setup embedded UI handler: %w", err)
	}
	mux.PathPrefix("/ui/").Handler(uiHandler)

	mux.PathPrefix("/").HandlerFunc(p2pServer.HandleLibp2p)

	startupSpan.End()
	slog.SetDefault(log.New(log.WithStdout(cmd.Bool(fDebug))))

	s := &http.Server{
		Handler: mux,
		Addr:    fmt.Sprintf(":%s", port),
	}

	eg, egCtx := errgroup.WithContext(notifyCtx)
	eg.Go(func() error {
		slog.InfoContext(egCtx, "starting sequencer")
		return eventStore.StartSequencer(egCtx)
	})
	eg.Go(func() error {
		slog.InfoContext(egCtx, "starting server", "port", port)
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
		slog.InfoContext(egCtx, "shutting down p2p server")
		if err := p2pServer.Close(); err != nil {
			slog.ErrorContext(egCtx, "error closing p2p host", "err", err)
		}
		slog.InfoContext(egCtx, "shutting down server")
		if err := s.Shutdown(context.Background()); err != nil {
			slog.ErrorContext(egCtx, "error shutting down http server", "err", err)
		}
		return nil
	})

	err = eg.Wait()
	if !errors.Is(err, context.Canceled) {
		slog.ErrorContext(notifyCtx, "server shut down returned an error", "err", err)
	}
	return err
}

func serveDid(domain string, hostKey atcrypto.PublicKey) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		did := syntax.DID(fmt.Sprintf("did:web:%s", domain))
		httpx.WriteJSON(r.Context(), w, identity.DIDDocument{
			DID: did,
			VerificationMethod: []identity.DocVerificationMethod{
				{
					ID:                 "habitat",
					Type:               "Multikey",
					Controller:         did.String(),
					PublicKeyMultibase: hostKey.Multibase(),
				},
			},
			Service: []identity.DocService{
				{
					ID:              "#habitat",
					ServiceEndpoint: "https://" + domain,
					Type:            "HabitatServer",
				},
			},
		})
	}
}

func setupFGA(ctx context.Context, cmd *cli.Command) (fgastore.Store, error) {
	dsn := cmd.String(fDB)
	// Share the main Postgres database for FGA when one is configured; only fall
	// back to a separate SQLite file when the main store is SQLite.
	if habitatdb.ParseDialect(dsn) == habitatdb.Postgres {
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
