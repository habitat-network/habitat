package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/contrib/bridges/otelzerolog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/gorilla/sessions"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/inbox"
	"github.com/habitat-network/habitat/internal/node"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/p2p"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/habitat-network/habitat/internal/pear"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"
	"github.com/habitat-network/habitat/internal/server"
	"github.com/habitat-network/habitat/internal/telemetry"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/habitat-network/habitat/internal/xrpcchannel"
	"github.com/urfave/cli/v3"
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
	otelClose, err := telemetry.SetupOpenTelemetry(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed setting up open telemetry for metric/trace/log collection")
	}
	log.Info().Msg("successfully set up open telemetry")
	// Handle shutdown properly so nothing leaks.
	defer otelClose(context.Background())

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
	hook := otelzerolog.NewHook("habitat" /* otel service name */, otelzerolog.WithLoggerProvider(global.GetLoggerProvider()))

	// Need to set log.Logger so globally anything initialized after here uses the global zerolog Logger
	// which is now hooked up to open telemetry.
	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger().Hook(hook)

	// Setup components
	db := setupDB(cmd)

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
	pdsClientFactory := pdsclient.NewHttpClientFactory(
		pdsCredStore,
		oauthClient,
		identity.DefaultDirectory(),
	)

	dir := identity.DefaultDirectory()
	node := setupNode(cmd, pdsClientFactory, dir)
	oauthServer := setupOAuthServer(cmd, node, db, oauthClient, pdsCredStore, meter)

	pear, err := setupPear(cmd, dir, node, db, oauthServer, pdsClientFactory)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup pear servers")
	}
	pearServer := server.NewServer(dir, pear, oauthServer, authn.NewServiceAuthMethod(dir))

	p2pServer, err := p2p.NewServer(authn.NewServiceAuthMethod(dir), pear, meter)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup p2p server")
	}

	// Create error group for managing goroutines
	eg, egCtx := errgroup.WithContext(ctx)
	mux := http.NewServeMux()

	// auth routes
	mux.HandleFunc("/oauth-callback", oauthServer.HandleCallback)
	mux.HandleFunc("/client-metadata.json", oauthServer.HandleClientMetadata)
	mux.HandleFunc("/oauth/authorize", oauthServer.HandleAuthorize)
	mux.HandleFunc("/oauth/token", oauthServer.HandleToken)
	mux.HandleFunc("/xrpc/network.habitat.listConnectedApps", oauthServer.ListConnectedApps)

	// pear routes
	mux.HandleFunc("/xrpc/network.habitat.putRecord", pearServer.PutRecord)
	mux.HandleFunc("/xrpc/network.habitat.getRecord", pearServer.GetRecord)
	mux.HandleFunc("/xrpc/network.habitat.listRecords", pearServer.ListRecords)
	mux.HandleFunc("/xrpc/network.habitat.repo.listCollections", pearServer.ListCollections)
	mux.HandleFunc("/xrpc/network.habitat.repo.deleteRecord", pearServer.DeleteRecord)

	mux.HandleFunc("/xrpc/network.habitat.uploadBlob", pearServer.UploadBlob)
	mux.HandleFunc("/xrpc/network.habitat.getBlob", pearServer.GetBlob)

	mux.HandleFunc("/xrpc/network.habitat.listPermissions", pearServer.ListPermissions)
	mux.HandleFunc("/xrpc/network.habitat.addPermission", pearServer.AddPermission)
	mux.HandleFunc("/xrpc/network.habitat.removePermission", pearServer.RemovePermission)

	mux.HandleFunc("/.well-known/did.json", serveDid(domain))

	pdsForwarding := newPDSForwarding(pdsCredStore, oauthServer, pdsClientFactory)
	mux.Handle("/xrpc/", pdsForwarding)

	// TODO: should we put this behind /p2p instead of / ?
	mux.HandleFunc("/", p2pServer.HandleLibp2p)

	otelMiddleware := otelhttp.NewMiddleware(
		"habitat-backend",
		// Add extra attributes to every span
		otelhttp.WithSpanNameFormatter(func(op string, r *http.Request) string {
			return r.Method + " " + r.URL.Path // e.g. "GET /users"
		}),
	)

	s := &http.Server{
		Handler: otelMiddleware(corsMiddleware(mux)),
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

func setupDB(cmd *cli.Command) *gorm.DB {
	postgresUrl := cmd.String(fPgUrl)
	if postgresUrl != "" {
		db, err := gorm.Open(postgres.Open(postgresUrl), &gorm.Config{})
		if err != nil {
			log.Fatal().Err(err).Msg("unable to open postgres db backing pear server")
		}
		return db
	}
	pearDB, err := gorm.Open(sqlite.Open(cmd.String(fDb)))
	if err != nil {
		log.Fatal().Err(err).Msg("unable to open sqlite file backing pear server")
	}
	return pearDB
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
	cmd *cli.Command,
	dir identity.Directory,
	node node.Node,
	db *gorm.DB,
	oauthServer *oauthserver.OAuthServer,
	clientFactory pdsclient.HttpClientFactory,
) (pear.Pear, error) {

	repo, err := repo.NewRepo(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create pear repo: %w", err)
	}

	permissions, err := permissions.NewStore(db, node)
	if err != nil {
		return nil, fmt.Errorf("failed to create permission store: %w", err)
	}

	inbox, err := inbox.New(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create inbox: %w", err)
	}

	return pear.NewPear(node, dir, permissions, repo, inbox), nil
}

func setupOAuthServer(
	cmd *cli.Command,
	node node.Node,
	db *gorm.DB,
	oauthClient pdsclient.PdsOAuthClient,
	credStore pdscred.PDSCredentialStore,
	meter metric.Meter,
) *oauthserver.OAuthServer {

	oauthServer, err := oauthserver.NewOAuthServer(
		cmd.String(fOauthServerSecret),
		oauthClient,
		sessions.NewCookieStore([]byte("my super secret signing password")),
		node,
		identity.DefaultDirectory(),
		credStore,
		db,
		meter,
	)
	if err != nil {
		log.Fatal().Err(err).Msgf("unable to setup oauth server")
	}
	return oauthServer
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().
			Set("Access-Control-Allow-Headers", "Content-Type, Authorization, habitat-auth-method, User-Agent, atproto-accept-labelers, at-proxy")
		w.Header().Set("Access-Control-Max-Age", "86400") // Cache preflight for 24 hours

		// Handle preflight OPTIONS request
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
