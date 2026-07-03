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
	"github.com/habitat-network/habitat/demos/fruitgang/internal/index"
	"github.com/habitat-network/habitat/demos/fruitgang/internal/indexer"
	"github.com/habitat-network/habitat/demos/fruitgang/internal/server"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/sap"
	"github.com/urfave/cli/v3"
	"golang.org/x/sync/errgroup"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	app := &cli.Command{
		Name:   "fruitgang-backend",
		Usage:  "Fruit Gang demo backend",
		Flags:  flags(),
		Action: start,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return app.Run(ctx, args)
}

var (
	fDB          = "db"
	fPort        = "port"
	fDomain      = "domain"
	fSecret      = "secret"
	fFrontendURL = "frontend-url"
)

func flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: fDB, Value: "sqlite://fruitgang.db", Sources: cli.EnvVars("FG_DB")},
		&cli.StringFlag{Name: fPort, Value: "3100", Sources: cli.EnvVars("FG_PORT")},
		&cli.StringFlag{Name: fDomain, Value: "fruitgang-api.local.habitat.network", Sources: cli.EnvVars("FG_DOMAIN")},
		&cli.StringFlag{Name: fSecret, Value: "", Sources: cli.EnvVars("FG_SECRET")},
		&cli.StringFlag{Name: fFrontendURL, Value: "https://fruitgang.local.habitat.network", Sources: cli.EnvVars("FG_FRONTEND_URL")},
	}
}

func start(ctx context.Context, cmd *cli.Command) error {
	fmt.Println("fdb", fDB)
	db, err := openDB(cmd.String(fDB))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	store, err := index.NewStore(db)
	if err != nil {
		return fmt.Errorf("init index store: %w", err)
	}

	secretStr := cmd.String(fSecret)
	oauthStore, err := oauthclient.NewGormStore(db)
	if err != nil {
		return fmt.Errorf("create oauth store: %w", err)
	}

	domain := cmd.String(fDomain)
	oauthCfg := oauth.NewPublicConfig(
		"https://"+domain+"/client-metadata.json",
		"https://"+domain+"/oauth-callback",
		[]string{"org:*"},
	)

	if secretStr != "" {
		secret, err := atcrypto.ParsePrivateMultibase(secretStr)
		if err != nil {
			return fmt.Errorf("parse secret: %w", err)
		}
		if err := oauthCfg.SetClientSecret(secret, "fruitgang"); err != nil {
			return fmt.Errorf("set client secret: %w", err)
		}
	}

	oauthApp := oauthclient.NewApp(&oauthCfg, oauthStore)

	s, err := sap.NewSap(sap.SapConfig{DB: db, OAuthClient: oauthApp})
	if err != nil {
		return fmt.Errorf("create sap: %w", err)
	}

	fg := newFruitgangServer(s, oauthApp, cmd.String(fFrontendURL))

	rootMux := http.NewServeMux()
	rootMux.HandleFunc("GET /client-metadata.json", fg.handleClientMetadata)
	rootMux.HandleFunc("POST /add-org", fg.handleAddOrg)
	rootMux.HandleFunc("OPTIONS /add-org", fg.handleAddOrgCORSPreflight)
	rootMux.HandleFunc("GET /oauth-callback", fg.handleOAuthCallback)
	rootMux.Handle("/", server.New(store, s))

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error { return s.Start(ctx) })
	eg.Go(func() error { return indexer.Run(ctx, s.Outbox, store) })
	eg.Go(func() error {
		srv := &http.Server{Addr: ":" + cmd.String(fPort), Handler: rootMux}
		go func() { _ = srv.ListenAndServe() }()
		<-ctx.Done()
		return srv.Shutdown(context.Background())
	})
	return eg.Wait()
}

func openDB(dsn string) (*gorm.DB, error) {
	if !strings.HasPrefix(dsn, "sqlite://") {
		return nil, fmt.Errorf("only sqlite:// DSNs are supported, got: %s", dsn)
	}
	path := strings.TrimPrefix(dsn, "sqlite://")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode=WAL;")
	db.Exec("PRAGMA synchronous=NORMAL;")
	db.Exec("PRAGMA busy_timeout=10000;")
	return db, nil
}
