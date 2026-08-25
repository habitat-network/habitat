package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity/apidir"
	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/log"
	"github.com/habitat-network/habitat/internal/telemetry"
	"github.com/habitat-network/habitat/pkg/oauthclient"
	"github.com/habitat-network/habitat/pkg/sap"
	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.ErrorContext(context.Background(), "error running command", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	app := &cli.Command{
		Name:   "sap",
		Usage:  "sync state tracker for habitat space events",
		Flags:  getFlags(),
		Action: runSap,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	return app.Run(ctx, args)
}

func runSap(ctx context.Context, cmd *cli.Command) error {
	otelShutdown, err := telemetry.SetupOpenTelemetry(ctx, "sap")
	if err != nil {
		return fmt.Errorf("setup opentelemetry: %w", err)
	}
	defer func() { _ = otelShutdown(context.Background()) }()

	slog.SetDefault(log.New(log.WithLevel(cmd.String(fLogLevel))))

	db, err := db.New(
		cmd.String(fDB),
		db.WithGORMConfig(&gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "sap_"}}),
	)
	if err != nil {
		return fmt.Errorf("setup database: %w", err)
	}

	secretStr := cmd.String(fSecret)
	secret, err := atcrypto.ParsePrivateMultibase(secretStr)
	if err != nil {
		return fmt.Errorf("parse secret: %w", err)
	}

	domain := cmd.String(fDomain)
	store, err := oauthclient.NewGormStore(db, oauthclient.WithSingleSessionPerUser())
	if err != nil {
		return fmt.Errorf("create oauth store: %w", err)
	}

	config := oauth.NewPublicConfig(
		"https://"+domain+"/client-metadata.json",
		"https://"+domain+"/oauth-callback",
		[]string{},
	)
	if err := config.SetClientSecret(secret, "sap"); err != nil {
		return fmt.Errorf("set client secret: %w", err)
	}

	oauthApp := oauth.NewClientApp(&config, store)
	oauthApp.Resolver.Client = httpx.NewClient()

	// When an identity resolver is configured, resolve identities through that
	// service instead of the public network. It rewrites the #atproto_pds
	// service entry of the DID documents it returns to point at itself, so
	// every PDS request sap makes is routed through it.
	if resolverURL := cmd.String(fIdentityResolver); resolverURL != "" {
		dir := apidir.NewAPIDirectory(strings.TrimSuffix(resolverURL, "/"))
		dir.Client = httpx.NewClient()
		oauthApp.Dir = dir
	}

	endpoint := "https://" + domain

	s, err := sap.New(sap.Config{
		DB:          db,
		OAuthClient: oauthApp,
		Directory:   oauthApp.Dir,
		// Endpoint is the base URL sap registers with each space host as
		// its notifyWrite delivery address (see pkg/sap/register), so
		// writes to tracked spaces reach sap's outbox live instead of only
		// being discovered on the next crawl.
		Endpoint: endpoint,
		Meter:    otel.Meter("sap"),
		Tracer:   otel.Tracer("sap"),
	})
	if err != nil {
		return fmt.Errorf("create sap: %w", err)
	}

	server := NewSapServer(s, oauthApp, endpoint)

	// The OAuth endpoints (callback and client metadata) must be publicly
	// reachable since the user's PDS redirects to them, so they are served on
	// their own port. The org and channel endpoints are served on a separate
	// internal port so the user can restrict access to trusted services.
	oauthMux := http.NewServeMux()
	oauthMux.HandleFunc("/oauth-callback", server.handleOAuthCallback)
	oauthMux.HandleFunc("/client-metadata.json", server.handleClientMetadata)
	oauthMux.HandleFunc("/xrpc/network.habitat.space.notifyWrite", server.handleNotifyWrite)

	internalMux := http.NewServeMux()
	internalMux.HandleFunc("/health", server.handleHealth)
	internalMux.HandleFunc("/session/add", server.handleAddSession)
	internalMux.HandleFunc("/session/list", server.handleListSessions)
	internalMux.HandleFunc("/space/track", server.handleTrackSpace)
	internalMux.HandleFunc("/session/recrawl", server.handleRecrawl)
	internalMux.HandleFunc("/channel", server.handleOutboxChannel)
	internalMux.HandleFunc("/proxy/", server.handleProxy)

	var internalHandler http.Handler = internalMux
	if internalAuthSecret := cmd.String(fInternalAuthSecret); internalAuthSecret != "" {
		internalHandler = basicAuthMiddleware(internalAuthSecret, internalMux)
	}

	port := cmd.String(fPort)
	internalPort := cmd.String(fInternalPort)

	slog.InfoContext(
		ctx, "listening",
		"oauth_port", port,
		"internal_port", internalPort,
	)

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		err := s.Start(ctx)
		slog.ErrorContext(ctx, "stopped", "error", err)
		return err
	})

	if port == internalPort {
		// Same port configured for both: share a single listener, with the
		// public OAuth routes taking precedence over the internal ones on
		// any overlapping pattern.
		combinedMux := http.NewServeMux()
		combinedMux.Handle("/", internalHandler)
		combinedMux.HandleFunc("/oauth-callback", server.handleOAuthCallback)
		combinedMux.HandleFunc("/client-metadata.json", server.handleClientMetadata)
		combinedMux.HandleFunc("/xrpc/network.habitat.space.notifyWrite", server.handleNotifyWrite)
		eg.Go(func() error {
			return serve(ctx, fmt.Sprintf(":%s", port), combinedMux)
		})
	} else {
		eg.Go(func() error {
			return serve(ctx, fmt.Sprintf(":%s", port), oauthMux)
		})
		eg.Go(func() error {
			return serve(ctx, fmt.Sprintf(":%s", internalPort), internalHandler)
		})
	}

	err = eg.Wait()
	return err
}

func serve(ctx context.Context, addr string, handler http.Handler) error {
	srv := http.Server{
		Addr:    addr,
		Handler: handler,
	}
	go func() { _ = srv.ListenAndServe() }()
	<-ctx.Done()
	return srv.Shutdown(ctx)
}
