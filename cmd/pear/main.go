package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/alexedwards/argon2id"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/clique"
	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/hive"
	habitat_identity "github.com/habitat-network/habitat/internal/identity"
	"github.com/habitat-network/habitat/internal/instance"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/org"
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
	"github.com/habitat-network/habitat/internal/utils"
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

	meter := otel.Meter("habitat-meter")

	gauge, err := meter.Int64Gauge("habitat.running", metric.WithUnit("item"))
	if err != nil {
		slog.Error("failed to create gauge", "err", err)
	} else {
		gauge.Record(startupCtx, 1)
		defer gauge.Record(context.Background(), 0)
	}

	slog.SetDefault(log.New(cmd.Bool(fDebug)))

	slog.Info("running with flags", "flags", cmd.FlagNames())

	db := db.New(cmd.String(fDb), db.WithMigrations(embedMigrations))
	fgaStore := setupFGA(startupCtx, cmd)

	oauthSecret, err := encrypt.ParseKey(cmd.String(fOauthServerSecret))
	if err != nil {
		slog.Error("unable to parse oauth server secret for login provider", "err", err)
		os.Exit(1)
	}

	passwordHash, err := setupInstanceAdminPassword(cmd)
	// Reuse the oauth server secret
	instanceAdminStore, err := instance.NewStore(
		db.WithContext(startupCtx),
		oauthSecret,
		fDomain,
		passwordHash,
	)
	if err != nil {
		slog.Error("unable to setup instance admin store", "err", err)
		os.Exit(1)
	}

	instanceAdminServer := instance.NewServer(instanceAdminStore, "habitat.network")

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
	mux.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			span := trace.SpanFromContext(r.Context())
			span.SetAttributes(attribute.String("http.request.header.referer", r.Referer()))
			next.ServeHTTP(w, r)
		})
	})
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

	hive, err := hive.NewHive(hiveDomain, domain, db.WithContext(startupCtx))
	if err != nil {
		slog.Error("unable to setup hive (identity service for org)", "err", err)
		os.Exit(1)
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
		slog.Error("unable to setup PDS client factory", "err", err)
		os.Exit(1)
	}

	passwordProvider, err := login.NewPasswordProvider(
		db,
		cmd.String(fDomain),
		oauthSecret,
		hiveDir,
	)
	if err != nil {
		slog.Error("unable to setup password login provider", "err", err)
		os.Exit(1)
	}
	orgStore, err := org.NewStore(
		db.WithContext(startupCtx),
		hive,
		hiveDir,
		domain,
		passwordProvider,
		fgaStore,
	)
	if err != nil {
		slog.Error("unable to setup org store", "err", err)
		os.Exit(1)
	}

	loginRouter := &org.LoginRouter{
		Pds:      login.NewPDSProvider(oauthClient, pdsCredStore, defaultDir),
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
		// OAuth server needs privileged access to lookup hive-hosted identities
		hiveDir,
		db.WithContext(startupCtx),
		meter,
		orgStore,
		"https://"+domain+"/oauth/token",
		oauthserver.NewJWTBearerStore(
			cmd.StringSlice(fBuiltinApps)...,
		),
	)
	if err != nil {
		slog.Error("unable to setup oauth server", "err", err)
		os.Exit(1)
	}

	// Implement service proxying https://atproto.com/specs/xrpc#service-proxying
	mux.Use(forwarding.NewServiceProxy(oauthServer, hive, hiveDir))

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
	serviceAuth := authn.NewServiceAuthMethod(defaultDir)
	spacesServer := spaces.NewServer(
		spacesStore,
		fgaStore,
		oauthServer,
		serviceAuth,
		orgStore,
	)

	relationshipStore := relationship.NewStore(db.WithContext(startupCtx), spacesStore, fgaStore)
	relationshipServer := relationship.NewServer(
		relationshipStore,
		fgaStore,
		oauthServer,
		serviceAuth,
	)

	repo, err := repo.NewRepo(db.WithContext(startupCtx))
	if err != nil {
		slog.Error("unable to setup repo", "err", err)
		os.Exit(1)
	}

	permissions, err := permissions.NewStore(db, cliqueStore)
	if err != nil {
		slog.Error("failed to create permission store", "err", err)
		os.Exit(1)
	}

	pear := pear.NewPear(hiveDir, permissions, repo)
	// Server for org management routes
	orgServer, err := org.NewServer(
		orgStore,
		oauthServer,
		pear,
		domain,
		hiveDir,
		instanceAdminStore,
	)
	if err != nil {
		slog.Error("unable to setup org server for domain", "err", err, "domain", domain)
		os.Exit(1)
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
		slog.Error("unable to setup p2p server", "err", err)
		os.Exit(1)
	}

	idServer, err := habitat_identity.NewServer(hive, oauthServer, orgStore)
	if err != nil {
		slog.Error("unable to setup hive server", "err", err)
		os.Exit(1)
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
		slog.Info("successfully set up waitlist service")
		mux.HandleFunc("/waitlist", waitlistSvc.HandleWaitlistEmailSignup)
	} else {
		slog.Error("unable to set up waitlist service", "err", err)
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

	pdsForwarding := forwarding.NewPDSForwarding(
		pdsCredStore,
		oauthServer,
		pdsClientFactory,
		defaultDir,
	)
	mux.PathPrefix("/xrpc/com.atproto.repo.").Handler(pdsForwarding)
	mux.PathPrefix("/xrpc/com.atproto.sync.").Handler(pdsForwarding)

	mux.HandleFunc(
		"/xrpc/com.atproto.server.getServiceAuth",
		func(w http.ResponseWriter, r *http.Request) {
			credInfo, ok := oauthServer.Validate(w, r)
			if !ok {
				return
			}
			id, err := hiveDir.LookupDID(r.Context(), credInfo.Subject)
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
				idServer.GetServiceAuth(w, r)
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

	uiHandler, err := webui.New(cmd.String(fUiDevProxy))
	if err != nil {
		slog.Error("unable to setup embedded UI handler", "err", err)
		os.Exit(1)
	}
	mux.PathPrefix("/ui/").Handler(uiHandler)

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

func setupInstanceAdminPassword(cmd *cli.Command) (string, error) {
	pass := cmd.String(fAdminPassword)

	// Generate a password on startup if not given
	generate := pass == ""
	if generate {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		pass = string(b)
		slog.Warn(
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
