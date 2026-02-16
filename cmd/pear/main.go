package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	jose "github.com/go-jose/go-jose/v3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/inbox"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/habitat-network/habitat/internal/pear"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/telemetry"
	"github.com/habitat-network/habitat/internal/userstore"
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
	keyFile := cmd.String(fKeyFile)
	domain := cmd.String(fDomain)
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

	// Metric that records a single running process (for testing)
	meter := otel.Meter("habitat-meter", metric.WithInstrumentationAttributes(attribute.KeyValue{
		Key:   "env",
		Value: attribute.StringValue("local"),
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
	mw := zerolog.MultiLevelWriter(
		os.Stdout,
		telemetry.NewOtelLogWriter(global.GetLoggerProvider().Logger("zerolog")),
	)

	// Need to set log.Logger so globally anything initialized after here uses the global zerolog Logger
	// which is now hooked up to open telemetry.
	log.Logger = zerolog.New(mw).With().Timestamp().Logger()

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
	userStore, err := userstore.NewUserStore(db)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup user store")
	}
	oauthServer, oauthClient := setupOAuthServer(keyFile, domain, pdsCredStore, userStore)
	pdsClientFactory := pdsclient.NewHttpClientFactory(
		pdsCredStore,
		oauthClient,
		identity.DefaultDirectory(),
	)

	serviceName := cmd.String(fServiceName)
	pearServer, err := setupPearServer(ctx, serviceName, domain, db, oauthServer)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup pear servers")
	}

	pdsForwarding := newPDSForwarding(pdsCredStore, oauthServer, pdsClientFactory)

	// Create error group for managing goroutines
	eg, egCtx := errgroup.WithContext(ctx)
	mux := http.NewServeMux()

	// auth routes
	mux.HandleFunc("/oauth-callback", oauthServer.HandleCallback)
	mux.HandleFunc("/client-metadata.json", oauthServer.HandleClientMetadata)
	mux.HandleFunc("/oauth/authorize", oauthServer.HandleAuthorize)
	mux.HandleFunc("/oauth/token", oauthServer.HandleToken)

	// pear routes
	mux.HandleFunc("/xrpc/network.habitat.putRecord", pearServer.PutRecord)
	mux.HandleFunc("/xrpc/network.habitat.getRecord", pearServer.GetRecord)
	mux.HandleFunc("/xrpc/network.habitat.listRecords", pearServer.ListRecords)

	mux.HandleFunc("/xrpc/network.habitat.uploadBlob", pearServer.UploadBlob)
	mux.HandleFunc("/xrpc/network.habitat.getBlob", pearServer.GetBlob)

	mux.HandleFunc("/xrpc/network.habitat.listPermissions", pearServer.ListPermissions)
	mux.HandleFunc("/xrpc/network.habitat.addPermission", pearServer.AddPermission)
	mux.HandleFunc("/xrpc/network.habitat.removePermission", pearServer.RemovePermission)

	mux.HandleFunc("/.well-known/did.json", func(w http.ResponseWriter, r *http.Request) {
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
	})

	mux.Handle("/xrpc/", pdsForwarding)

	otelMiddleware := otelhttp.NewMiddleware("habitat-backend" /* TODO: any options here? */)

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
		log.Info().Msg("shutting down server")
		return s.Shutdown(context.Background())
	})

	// Wait for all goroutines to finish
	err = eg.Wait()
	if err != nil {
		log.Err(err).Msgf("server shut down returned an error")
	}
	return err
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

func setupPearServer(
	ctx context.Context,
	serviceName string,
	domain string,
	db *gorm.DB,
	oauthServer *oauthserver.OAuthServer,
) (*pear.Server, error) {
	repo, err := pear.NewRepo(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create pear repo: %w", err)
	}

	permissions, err := permissions.NewStore(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create permission store: %w", err)
	}

	inbox, err := inbox.New(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create inbox: %w", err)
	}

	dir := identity.DefaultDirectory()
	p := pear.NewPear(ctx, domain, serviceName, dir, permissions, repo, inbox)
	return pear.NewServer(dir, p, oauthServer), nil
}

func setupOAuthServer(
	keyFile, domain string,
	credStore pdscred.PDSCredentialStore,
	userStore userstore.UserStore,
) (*oauthserver.OAuthServer, pdsclient.PdsOAuthClient) {
	var jwkBytes []byte
	_, err := os.Stat(keyFile)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Fatal().Err(err).Msgf("error finding key file")
		}
		// Generate ECDSA key using P-256 curve with crypto/rand for secure randomness
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			log.Fatal().Err(err).Msgf("failed to generate key")
		}
		// Create JWK from the generated key
		jwk := jose.JSONWebKey{
			Key:       key,
			KeyID:     "habitat",
			Algorithm: string(jose.ES256),
			Use:       "sig",
		}
		jwkBytes, err = json.MarshalIndent(jwk, "", "  ")
		if err != nil {
			log.Fatal().Err(err).Msgf("failed to marshal JWK")
		}
		if err := os.WriteFile(keyFile, jwkBytes, 0o600); err != nil {
			log.Fatal().Err(err).Msgf("failed to write key to file")
		}
		log.Info().Msgf("created key file at %s", keyFile)
	} else {
		// Read JWK from file
		jwkBytes, err = os.ReadFile(keyFile)
		if err != nil {
			log.Fatal().Err(err).Msgf("failed to read key from file")
		}
	}

	jwk := &jose.JSONWebKey{}
	err = json.Unmarshal(jwkBytes, jwk)
	if err != nil {
		log.Fatal().Err(err).Msgf("failed to unmarshal JWK")
	}
	oauthClient := pdsclient.NewPdsOAuthClient(
		"https://"+domain+"/client-metadata.json", /*clientId*/
		"https://"+domain,                         /*clientUri*/
		"https://"+domain+"/oauth-callback",       /*redirectUri*/
		jwk,
	)
	oauthServer := oauthserver.NewOAuthServer(
		jwk,
		oauthClient,
		sessions.NewCookieStore([]byte("my super secret signing password")),
		identity.DefaultDirectory(),
		credStore,
		userStore,
	)
	if err != nil {
		log.Fatal().Err(err).Msgf("unable to setup oauth server")
	}
	return oauthServer, oauthClient
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().
			Set("Access-Control-Allow-Headers", "Content-Type, Authorization, habitat-auth-method, User-Agent, atproto-accept-labelers")
		w.Header().Set("Access-Control-Max-Age", "86400") // Cache preflight for 24 hours

		// Handle preflight OPTIONS request
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
