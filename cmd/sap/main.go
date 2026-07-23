package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/log"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/sap"
	"github.com/habitat-network/habitat/internal/telemetry"
	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"
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

	db, err := db.New(cmd.String(fDB))
	if err != nil {
		return fmt.Errorf("setup database: %w", err)
	}

	secretStr := cmd.String(fSecret)
	secret, err := atcrypto.ParsePrivateMultibase(secretStr)
	if err != nil {
		return fmt.Errorf("parse secret: %w", err)
	}

	domain := cmd.String(fDomain)
	store, err := oauthclient.NewGormStore(db)
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

	dir := identity.DefaultDirectory()
	// The base URL space hosts register and sign notifyWrite /
	// notifySpaceDeleted against; the XRPC handlers hang off it.
	endpoint := "https://" + domain

	s, err := sap.New(sap.Config{
		DB:          db,
		OAuthClient: oauthApp,
		Directory:   dir,
		Endpoint:    endpoint,
		Meter:       otel.Meter("sap"),
		Tracer:      otel.Tracer("sap"),
	})
	if err != nil {
		return fmt.Errorf("create sap: %w", err)
	}

	server := NewSapServer(s, oauthApp, &auth.ServiceAuthValidator{
		Dir:      dir,
		Audience: endpoint,
	})

	// The OAuth endpoints (callback and client metadata) must be publicly
	// reachable since the user's PDS redirects to them, so they are served on
	// their own port. The session and channel endpoints are served on a separate
	// internal port so the user can restrict access to trusted services.
	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/oauth-callback", server.handleOAuthCallback)
	publicMux.HandleFunc("/client-metadata.json", server.handleClientMetadata)
	publicMux.HandleFunc("/xrpc/network.habitat.space.notifyWrite", server.handleNotifyWrite)
	publicMux.HandleFunc(
		"/xrpc/network.habitat.space.notifySpaceDeleted",
		server.handleNotifySpaceDeleted,
	)

	internalMux := http.NewServeMux()
	internalMux.HandleFunc("/health", server.handleHealth)
	internalMux.HandleFunc("/session/add", server.handleAddSession)
	internalMux.HandleFunc("/session/list", server.handleListSessions)
	internalMux.HandleFunc("/channel", server.handleOutboxChannel)
	internalMux.HandleFunc("/proxy/", server.handleProxy)

	slog.InfoContext(ctx, "listening",
		"oauth_port", cmd.String(fPort),
		"internal_port", cmd.String(fInternalPort),
	)

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		err := s.Start(ctx)
		slog.ErrorContext(ctx, "stopped", "error", err)
		return err
	})
	eg.Go(func() error {
		return serve(ctx, fmt.Sprintf(":%s", cmd.String(fPort)), publicMux)
	})
	eg.Go(func() error {
		return serve(ctx, fmt.Sprintf(":%s", cmd.String(fInternalPort)), internalMux)
	})

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
