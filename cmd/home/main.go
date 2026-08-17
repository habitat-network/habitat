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
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/log"
	"github.com/habitat-network/habitat/internal/telemetry"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/pkg/oauthclient"
	"github.com/habitat-network/habitat/pkg/sap"
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
	ctx := context.Background()
	if err := app.Run(ctx, os.Args); err != nil {
		slog.ErrorContext(ctx, "error running command", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	otelShutdown, err := telemetry.SetupOpenTelemetry(ctx, "home")
	if err != nil {
		return fmt.Errorf("setup opentelemetry: %w", err)
	}
	defer func() { _ = otelShutdown(context.Background()) }()

	slog.SetDefault(log.New(log.WithLevel(cmd.String(fLogLevel)), log.WithStdout(true)))

	gormDB, err := db.New(cmd.String(fDB))
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

	oauthStore, err := oauthclient.NewGormStore(gormDB, oauthclient.WithSingleSessionPerUser())
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
	oauthApp := oauth.NewClientApp(&config, oauthStore)
	// oauth.NewClientApp's default Resolver dials over an SSRF-guarded
	// transport that rejects loopback/private addresses, which local
	// *.local.habitat.network domains resolve to. sap's credential manager
	// hits the same problem reaching local dev PDS hosts and works around it
	// the same way: use a plain client instead of the guarded one. Retries
	// are layered on top since this server and pear start concurrently in
	// dev: home can hit pear before pear's listener is up, and a bare 502
	// from Caddy shouldn't be fatal.
	oauthApp.Resolver.Client = httpx.NewClient(httpx.WithRetry())
	// oauthApp.Dir independently defaults to its own identity.DefaultDirectory()
	// (a *CacheDirectory wrapping a *BaseDirectory); unpack it the same way
	// NewClientApp itself does for UserAgent, to apply the same retry client to
	// identity (DID/handle) resolution — the JWT bearer bootstrap below and
	// GroupService.resolveSubject both depend on it tolerating a transient 5xx
	// rather than failing outright.
	if cdir, ok := oauthApp.Dir.(*identity.CacheDirectory); ok {
		if bdir, ok := cdir.Inner.(*identity.BaseDirectory); ok {
			bdir.HTTPClient = *httpx.NewClient(httpx.WithRetry())
		}
	}

	s, err := sap.New(sap.Config{
		DB:          gormDB,
		Directory:   dir,
		OAuthClient: oauthApp,
	})
	if err != nil {
		return fmt.Errorf("create sap: %w", err)
	}

	store, err := NewStore(gormDB)
	if err != nil {
		return fmt.Errorf("create store: %w", err)
	}

	groups := NewGroupService(store, oauthApp)
	collections := NewCollectionService(store, oauthApp)
	orgs := NewOrgService(oauthApp, s, store)
	indexer := NewIndexer(store, s.Outbox())
	server := NewServer(
		domain,
		cmd.String(fOrg),
		groups,
		collections,
		orgs,
		oauthApp,
		s,
		store,
		authn.NewServiceAuthMethod(
			org.NewEveryoneOrg(domain),
			dir,
			"did:web:"+domain+"#"+serviceID,
		),
	)

	mux := http.NewServeMux()
	server.Routes(mux)

	addr := ":" + cmd.String(fPort)
	srv := &http.Server{Addr: addr, Handler: mux}

	// No org is bootstrapped at startup: home only learns about an org (and
	// authenticates its credential via the JWT Bearer grant) on demand, via
	// network.habitat.org.requestCrawl — see OrgService.RequestCrawl.
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
