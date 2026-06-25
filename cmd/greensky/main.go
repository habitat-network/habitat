package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/gorilla/mux"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/sap"
	"github.com/urfave/cli/v3"
	"golang.org/x/sync/errgroup"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func main() {
	cmd := &cli.Command{
		Name:   "greensky",
		Usage:  "Greensky discussion-forum appview server",
		Flags:  getFlags(),
		Action: run,
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatalf("Error running command: %v", err)
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(strings.ToUpper(cmd.String(fLogLevel)))); err != nil {
		return fmt.Errorf("unmarshal log level: %w", err)
	}
	slog.SetLogLoggerLevel(logLevel)

	db, err := setupDatabase(cmd.String(fDB))
	if err != nil {
		return fmt.Errorf("setup database: %w", err)
	}

	store, err := NewPostStore(db)
	if err != nil {
		return fmt.Errorf("setup post store: %w", err)
	}

	dir := newDirectory(cmd.Bool(fInsecureTLS))

	s, err := sap.NewSap(sap.SapConfig{
		DB:           db,
		PublicDomain: cmd.String(fDomain),
		Secret:       cmd.String(fSecret),
		Directory:    dir,
	})
	if err != nil {
		return fmt.Errorf("setup sap: %w", err)
	}

	didCfg := newDIDConfig(cmd.String(fDomain))
	serviceAuth := authn.NewServiceAuthMethod(dir)
	server := NewServer(store, s, serviceAuth, didCfg)
	ingester := NewIngester(store, s)

	router := mux.NewRouter()
	router.HandleFunc("/xrpc/network.habitat.greensky.getPosts", server.HandleGetPosts).
		Methods(http.MethodGet)
	router.HandleFunc("/org/add", server.HandleAddOrg).Methods(http.MethodGet)
	router.HandleFunc("/.well-known/did.json", server.HandleDIDDoc).Methods(http.MethodGet)
	router.HandleFunc("/health", server.HandleHealth).Methods(http.MethodGet)
	// sap owns the OAuth client metadata + callback used to grant the org credential.
	router.Handle("/oauth-callback", s)
	router.Handle("/client-metadata.json", s)

	addr := fmt.Sprintf(":%s", cmd.String(fPort))
	srv := &http.Server{Addr: addr, Handler: router}

	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	eg, egCtx := errgroup.WithContext(sigCtx)
	eg.Go(func() error {
		return s.Start(egCtx)
	})
	eg.Go(func() error {
		return ingester.Run(egCtx)
	})
	eg.Go(func() error {
		slog.InfoContext(egCtx, "starting greensky server", "addr", addr)
		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.ErrorContext(egCtx, "server error", "err", err)
			}
		}()
		<-egCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	})

	return eg.Wait()
}

func setupDatabase(dbURL string) (*gorm.DB, error) {
	if !strings.HasPrefix(dbURL, "sqlite://") {
		return nil, fmt.Errorf("unsupported database URL: %s (only sqlite:// supported)", dbURL)
	}
	path := dbURL[len("sqlite://"):]
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode=WAL;")
	db.Exec("PRAGMA synchronous=NORMAL;")
	db.Exec("PRAGMA busy_timeout=10000;")
	return db, nil
}

// newDirectory builds the identity directory greensky uses to resolve did:web
// documents — both the org handle during onboarding and the caller's signing
// key during service-auth verification. SkipHandleVerification is fine because
// greensky authenticates by DID, not handle. With insecureTLS set (local dev
// only) it trusts Caddy's self-signed certs for *.local.habitat.network.
func newDirectory(insecureTLS bool) identity.Directory {
	transport := &http.Transport{
		IdleConnTimeout: time.Second,
		MaxIdleConns:    100,
	}
	if insecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &identity.BaseDirectory{
		PLCURL: identity.DefaultPLCURL,
		HTTPClient: http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		},
		TryAuthoritativeDNS:    true,
		SkipDNSDomainSuffixes:  []string{".bsky.social"},
		SkipHandleVerification: true,
	}
}
