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

	"github.com/habitat-network/habitat/internal/sap"
	"github.com/urfave/cli/v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
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

	db, err := setupDatabase(cmd.String(fDb))
	if err != nil {
		return fmt.Errorf("setup database: %w", err)
	}

	s, err := sap.NewSap(sap.SapConfig{
		DB:           db,
		PublicDomain: cmd.String(fDomain),
		Secret:       cmd.String(fSecret),
	})
	if err != nil {
		return fmt.Errorf("create sap: %w", err)
	}

	server := NewSapServer(s)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", server.handleHealth)
	mux.HandleFunc("/org/add", server.handleAddOrg)
	mux.HandleFunc("/org/list", server.handleListOrgs)
	mux.Handle("/oauth-callback", s)
	mux.Handle("/client-metadata.json", s)

	slog.InfoContext(ctx, "listening", "port", cmd.String(fPort))
	return http.ListenAndServe(":"+cmd.String(fPort), mux)
}

func setupDatabase(dbURL string) (*gorm.DB, error) {
	if !strings.HasPrefix(dbURL, "sqlite://") {
		return nil, fmt.Errorf("unsupported database URL: %s (only sqlite:// supported)", dbURL)
	}

	path := dbURL[len("sqlite://"):]
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	db.Exec("PRAGMA journal_mode=WAL;")
	db.Exec("PRAGMA synchronous=NORMAL;")
	db.Exec("PRAGMA busy_timeout=10000;")

	return db, nil
}
