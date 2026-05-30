package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"
)

type Config struct {
	PearURL           string
	DatabaseURL       string
	Bind              string
	SpaceTypes        []string
	CollectionFilters []string
	AccessToken       string
	WebhookURL        string
	DisableAcks       bool
	ResyncParallelism int
	OutboxParallelism int
	LogLevel          string
}

func run(ctx context.Context, cfg *Config) error {
	slog.Info("starting spacetap", "pear_url", cfg.PearURL, "bind", cfg.Bind)
	<-ctx.Done()
	slog.Info("shutting down spacetap")
	return nil
}

func main() {
	cfg := &Config{}

	cmd := &cli.Command{
		Name:  "spacetap",
		Usage: "Space sync client — syncs Pear spaces to local SQLite and delivers events to apps",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "pear-url",
				Usage:       "Pear server URL",
				Sources:     cli.EnvVars("SPACETAP_PEAR_URL"),
				Value:       "http://localhost:8000",
				Destination: &cfg.PearURL,
			},
			&cli.StringFlag{
				Name:        "database-url",
				Usage:       "SQLite database path",
				Sources:     cli.EnvVars("SPACETAP_DATABASE_URL"),
				Value:       "spacetap.db",
				Destination: &cfg.DatabaseURL,
			},
			&cli.StringFlag{
				Name:        "bind",
				Usage:       "HTTP server bind address",
				Sources:     cli.EnvVars("SPACETAP_BIND"),
				Value:       ":8080",
				Destination: &cfg.Bind,
			},
			&cli.StringSliceFlag{
				Name:        "space-types",
				Usage:       "Filter space types (repeatable)",
				Sources:     cli.EnvVars("SPACETAP_SPACE_TYPES"),
				Destination: (*[]string)(&cfg.SpaceTypes),
			},
			&cli.StringSliceFlag{
				Name:        "collection-filters",
				Usage:       "Filter collections (repeatable)",
				Sources:     cli.EnvVars("SPACETAP_COLLECTION_FILTERS"),
				Destination: (*[]string)(&cfg.CollectionFilters),
			},
			&cli.StringFlag{
				Name:        "access-token",
				Usage:       "OAuth access token for Pear API",
				Sources:     cli.EnvVars("SPACETAP_ACCESS_TOKEN"),
				Destination: &cfg.AccessToken,
			},
			&cli.StringFlag{
				Name:        "webhook-url",
				Usage:       "Webhook URL for event delivery",
				Sources:     cli.EnvVars("SPACETAP_WEBHOOK_URL"),
				Destination: &cfg.WebhookURL,
			},
			&cli.BoolFlag{
				Name:        "disable-acks",
				Usage:       "Disable ack tracking for outbox",
				Sources:     cli.EnvVars("SPACETAP_DISABLE_ACKS"),
				Destination: &cfg.DisableAcks,
			},
			&cli.IntFlag{
				Name:        "resync-parallelism",
				Usage:       "Number of parallel resync workers",
				Sources:     cli.EnvVars("SPACETAP_RESYNC_PARALLELISM"),
				Value:       2,
				Destination: &cfg.ResyncParallelism,
			},
			&cli.IntFlag{
				Name:        "outbox-parallelism",
				Usage:       "Number of parallel outbox workers",
				Sources:     cli.EnvVars("SPACETAP_OUTBOX_PARALLELISM"),
				Value:       4,
				Destination: &cfg.OutboxParallelism,
			},
			&cli.StringFlag{
				Name:        "log-level",
				Usage:       "Log level (debug, info, warn, error)",
				Sources:     cli.EnvVars("SPACETAP_LOG_LEVEL"),
				Value:       "info",
				Destination: &cfg.LogLevel,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			return run(ctx, cfg)
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
