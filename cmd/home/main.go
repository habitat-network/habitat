package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/log"
	"github.com/habitat-network/habitat/internal/telemetry"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/sap"
	"github.com/urfave/cli/v3"
	"golang.org/x/sync/errgroup"
)

func main() {
	app := &cli.Command{
		Name:   "home",
		Usage:  "Habitat home server: syncs and indexes group spaces and serves the groups API",
		Flags:  getFlags(),
		Action: run,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := app.Run(ctx, os.Args); err != nil {
		slog.ErrorContext(ctx, "error running command", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	otelShutdown, err := telemetry.SetupOpenTelemetry(ctx, "home")
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
	dir := identity.DefaultDirectory()

	oauthStore, err := oauthclient.NewGormStore(db)
	if err != nil {
		return fmt.Errorf("create oauth store: %w", err)
	}
	config := oauth.NewPublicConfig(
		"https://"+domain+"/client-metadata.json",
		"https://"+domain+"/oauth-callback",
		// Request no scope, matching pear's own management-plane client: pear's
		// scope grammar rejects the bare "atproto" scope, and its XRPC handlers
		// authorize by org/FGA role rather than by OAuth scope, so the org
		// credential can create spaces and write records/tuples without one.
		[]string{},
	)
	if err := config.SetClientSecret(secret, "home"); err != nil {
		return fmt.Errorf("set client secret: %w", err)
	}
	oauthApp := oauthclient.NewApp(&config, oauthStore)

	s, err := sap.NewSap(sap.SapConfig{
		DB:          db,
		Directory:   dir,
		OAuthClient: oauthApp,
	})
	if err != nil {
		return fmt.Errorf("create sap: %w", err)
	}

	store, err := NewStore(db)
	if err != nil {
		return fmt.Errorf("create store: %w", err)
	}

	groups := NewGroupService(store, oauthApp)
	collections := NewCollectionService(store, oauthApp)
	indexer := NewIndexer(store, s.Outbox)
	server := NewServer(
		domain, cmd.String(fOrgHandle), groups, collections, oauthApp, s, store,
		authn.NewServiceAuthMethod(dir, "did:web:"+domain+"#"+serviceID),
	)

	mux := http.NewServeMux()
	server.Routes(mux)

	addr := ":" + cmd.String(fPort)
	srv := &http.Server{Addr: addr, Handler: mux}

	if _, _, err := store.OrgSession(ctx); err != nil {
		slog.WarnContext(
			ctx,
			"home server not yet authorized for an org; visit /oauth/login to grant the org credential",
			"loginURL",
			"https://"+domain+"/oauth/login",
		)
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error { return s.Start(egCtx) })
	eg.Go(func() error { return indexer.Run(egCtx) })
	eg.Go(func() error {
		slog.InfoContext(egCtx, "home server listening", "addr", addr, "did", "did:web:"+domain)
		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.ErrorContext(egCtx, "server error", "err", err)
			}
		}()
		<-egCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	})

	return eg.Wait()
}
