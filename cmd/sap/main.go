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
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/sap"
	"github.com/habitat-network/habitat/internal/telemetry"
	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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
	var logLevel slog.Level
	err := logLevel.UnmarshalText([]byte(strings.ToUpper(cmd.String(fLogLevel))))
	if err != nil {
		return fmt.Errorf("unmarshal log level: %w", err)
	}
	slog.SetLogLoggerLevel(logLevel)

	otelShutdown, err := telemetry.SetupOpenTelemetry(ctx)
	if err != nil {
		return fmt.Errorf("setup opentelemetry: %w", err)
	}
	defer func() { _ = otelShutdown(context.Background()) }()

	db, err := setupDatabase(cmd.String(fDb))
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

	oauthApp := oauthclient.NewApp(&config, store)

	s, err := sap.NewSap(sap.SapConfig{
		DB:          db,
		OAuthClient: oauthApp,
		Meter:       otel.Meter("sap"),
		Tracer:      otel.Tracer("sap"),
	})
	if err != nil {
		return fmt.Errorf("create sap: %w", err)
	}

	server := NewSapServer(s, oauthApp)

	// The OAuth endpoints (callback and client metadata) must be publicly
	// reachable since the user's PDS redirects to them, so they are served on
	// their own port. The org and channel endpoints are served on a separate
	// internal port so the user can restrict access to trusted services.
	oauthMux := http.NewServeMux()
	oauthMux.HandleFunc("/oauth-callback", server.handleOAuthCallback)
	oauthMux.HandleFunc("/client-metadata.json", server.handleClientMetadata)
	oauthMux.HandleFunc("/xrpc/com.atproto.space.notifyWrite", s.HandleNotifyWrite)

	internalMux := http.NewServeMux()
	internalMux.HandleFunc("/health", server.handleHealth)
	internalMux.HandleFunc("/org/add", server.handleAddOrg)
	internalMux.HandleFunc("/org/list", server.handleListOrgs)
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
		return serve(ctx, fmt.Sprintf(":%s", cmd.String(fPort)), oauthMux)
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

func setupDatabase(dbURL string) (*gorm.DB, error) {
	if !strings.HasPrefix(dbURL, "sqlite://") {
		return nil, fmt.Errorf("unsupported database URL: %s (only sqlite:// supported)", dbURL)
	}

	path := dbURL[len("sqlite://"):]
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, err
	}

	db.Exec("PRAGMA journal_mode=WAL;")
	db.Exec("PRAGMA synchronous=NORMAL;")
	db.Exec("PRAGMA busy_timeout=10000;")

	return db, nil
}
