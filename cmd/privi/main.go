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
	"syscall"

	jose "github.com/go-jose/go-jose/v3"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/jetstream/pkg/client"
	"github.com/eagraf/habitat-new/internal/encrypt"
	"github.com/eagraf/habitat-new/internal/oauthclient"
	"github.com/eagraf/habitat-new/internal/oauthserver"
	"github.com/eagraf/habitat-new/internal/pdscred"
	"github.com/eagraf/habitat-new/internal/permissions"
	"github.com/eagraf/habitat-new/internal/privi"
	"github.com/gorilla/sessions"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

const (
	JetstreamURL       = "wss://jetstream2.us-east.bsky.network/subscribe"
	JetstreamUserAgent = "habitat-jetstream-client/v0.0.1"
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

	// Log the parsed flags
	log.Info().Msgf("running with flags: ")
	for _, flag := range cmd.FlagNames() {
		log.Info().Msgf("%s: %v", flag, cmd.Value(flag))
	}

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
	oauthServer := setupOAuthServer(keyFile, domain, pdsCredStore)
	priviServer := setupPriviServer(db, pdsCredStore, oauthServer)
	pdsForwarding := newPDSForwarding(pdsCredStore, oauthServer)

	// Setup context with signal handling for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Setup OpenTelemetry
	otelClose, err := setupOTelSDK(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed setting up telemetry")
	}

	meter := otel.Meter("habitat-meter", metric.WithInstrumentationAttributes(attribute.KeyValue{
		Key: "env",
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

	// Handle shutdown properly so nothing leaks.
	defer otelClose(context.Background())

	// Create error group for managing goroutines
	eg, egCtx := errgroup.WithContext(ctx)

	// Start the notification listener in a separate goroutine
	eg.Go(func() error {
		// Note that for now, we're ingesting all notifications in the entire system
		// This can be reduced in the future to only listen for DIDs that the user is interested in.
		config := &client.ClientConfig{
			Compress:          true,
			WebsocketURL:      "wss://jetstream2.us-east.bsky.network/subscribe",
			WantedDids:        []string{},
			WantedCollections: []string{"network.habitat.notification"},
			MaxSize:           0,
			ExtraHeaders: map[string]string{
				"User-Agent": "habitat-jetstream-client/v0.0.1",
			},
		}

		log.Info().Msg("starting notification listener")
		ingester, err := privi.NewNotificationIngester(db)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to setup notification ingester")
		}
		return privi.StartNotificationListener(egCtx, config, nil, ingester.GetEventHandler(), db)
	})

	mux := http.NewServeMux()

	// auth routes
	mux.HandleFunc("/oauth-callback", oauthServer.HandleCallback)
	mux.HandleFunc("/client-metadata.json", oauthServer.HandleClientMetadata)
	mux.HandleFunc("/oauth/authorize", oauthServer.HandleAuthorize)
	mux.HandleFunc("/oauth/token", oauthServer.HandleToken)

	// privi routes
	mux.HandleFunc("/xrpc/network.habitat.putRecord", priviServer.PutRecord)
	mux.HandleFunc("/xrpc/network.habitat.getRecord", priviServer.GetRecord)
	mux.HandleFunc("/xrpc/network.habitat.listRecords", priviServer.ListRecords)

	mux.HandleFunc("/xrpc/network.habitat.uploadBlob", priviServer.UploadBlob)
	mux.HandleFunc("/xrpc/network.habitat.getBlob", priviServer.GetBlob)

	mux.HandleFunc("/xrpc/network.habitat.listPermissions", priviServer.ListPermissions)
	mux.HandleFunc("/xrpc/network.habitat.addPermission", priviServer.AddPermission)
	mux.HandleFunc("/xrpc/network.habitat.removePermission", priviServer.RemovePermission)

	mux.HandleFunc(
		"/xrpc/network.habitat.notification.listNotifications",
		priviServer.ListNotifications,
	)
	mux.HandleFunc(
		"/xrpc/network.habitat.notification.createNotification",
		priviServer.CreateNotification,
	)

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

	otelMiddleware := otelhttp.NewMiddleware("habitat-backend", /* TODO: any options here? */)

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
	return eg.Wait()
}

func setupDB(cmd *cli.Command) *gorm.DB {
	postgresUrl := cmd.String(fPgUrl)
	if postgresUrl != "" {
		db, err := gorm.Open(postgres.Open(postgresUrl), &gorm.Config{})
		if err != nil {
			log.Fatal().Err(err).Msg("unable to open postgres db backing privi server")
		}
		return db
	}
	priviDB, err := gorm.Open(sqlite.Open(cmd.String(fDb)))
	if err != nil {
		log.Fatal().Err(err).Msg("unable to open sqlite file backing privi server")
	}
	return priviDB
}

func setupPriviServer(
	db *gorm.DB,
	credStore pdscred.PDSCredentialStore,
	oauthServer *oauthserver.OAuthServer,
) *privi.Server {
	repo, err := privi.NewSQLiteRepo(db)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup privi sqlite db")
	}

	permissionStore, err := permissions.NewSQLiteStore(db)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup permissions store")
	}

	inbox := privi.NewInbox(db)

	return privi.NewServer(permissionStore, repo, inbox, oauthServer, credStore)
}

func setupOAuthServer(
	keyFile, domain string,
	credStore pdscred.PDSCredentialStore,
) *oauthserver.OAuthServer {
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
	oauthClient := oauthclient.NewOAuthClient(
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
