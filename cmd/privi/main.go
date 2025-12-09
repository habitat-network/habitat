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
	"syscall"

	jose "github.com/go-jose/go-jose/v3"
	"golang.org/x/sync/errgroup"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/jetstream/pkg/client"
	"github.com/eagraf/habitat-new/internal/oauthclient"
	"github.com/eagraf/habitat-new/internal/oauthserver"
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
	dbPath := cmd.String(fDb)
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
	db := setupDB(dbPath)
	oauthServer := setupOAuthServer(keyFile, domain)
	priviServer := setupPriviServer(db, oauthServer)
	pdsForwarding := newPDSForwarding(oauthServer)

	// Setup context with signal handling for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

	s := &http.Server{
		Handler: corsMiddleware(loggingMiddleware(mux)),
		Addr:    fmt.Sprintf(":%s", port),
	}

	// Start the HTTP server in a goroutine
	eg.Go(func() error {
		log.Info().Msgf("starting server on port :%s", port)
		if httpsCerts == "" {
			return s.ListenAndServe()
		}
		return s.ListenAndServeTLS(
			fmt.Sprintf("%s%s", httpsCerts, "fullchain.pem"),
			fmt.Sprintf("%s%s", httpsCerts, "privkey.pem"),
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

func setupDB(dbPath string) *gorm.DB {
	priviDB, err := gorm.Open(sqlite.Open(dbPath))
	if err != nil {
		log.Fatal().Err(err).Msg("unable to open sqlite file backing privi server")
	}
	return priviDB
}

func setupPriviServer(db *gorm.DB, oauthServer *oauthserver.OAuthServer) *privi.Server {
	repo, err := privi.NewSQLiteRepo(db)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup privi sqlite db")
	}

	adapter, err := permissions.NewSQLiteStore(db)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup permissions store")
	}
	return privi.NewServer(adapter, repo, oauthServer)
}

func setupOAuthServer(keyFile, domain string) *oauthserver.OAuthServer {
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

	oauthClient, err := oauthclient.NewOAuthClient(
		"https://"+domain+"/client-metadata.json", /*clientId*/
		"https://"+domain,                         /*clientUri*/
		"https://"+domain+"/oauth-callback",       /*redirectUri*/
		jwkBytes,                                  /*secretJwk*/
	)
	if err != nil {
		log.Fatal().Err(err).Msgf("unable to setup oauth client")
	}

	oauthServer := oauthserver.NewOAuthServer(
		oauthClient,
		sessions.NewCookieStore([]byte("my super secret signing password")),
		identity.DefaultDirectory(),
	)
	return oauthServer
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().
			Set("Access-Control-Allow-Headers", "Content-Type, Authorization, habitat-auth-method, User-Agent")
		w.Header().Set("Access-Control-Max-Age", "86400") // Cache preflight for 24 hours

		// Handle preflight OPTIONS request
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
