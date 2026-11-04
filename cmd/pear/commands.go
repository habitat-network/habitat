package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/cmd/pear/migrations"
	"github.com/habitat-network/habitat/internal/log"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/habitat-network/habitat/internal/telemetry"
)

func commands() []*cli.Command {
	return []*cli.Command{
		{
			Name:   "serve",
			Usage:  "Run the pear server",
			Action: serve,
		},
		{
			Name:   "migrate",
			Usage:  "Apply pending database migrations and exit",
			Action: migrate,
		},
		{
			Name:  "run",
			Usage: "Run a task (backfills and other operator-run work); see `pear run --help`",
			// Each task is a subcommand, so adding one is adding it to taskCommands.
			Commands: taskCommands(),
		},
	}
}

func migrate(ctx context.Context, cmd *cli.Command) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, err := setupComponents(ctx, cmd)
	if err != nil {
		return err
	}
	defer func() { _ = app.Close() }()
	if err := migrations.Run(ctx, migrations.PearMigrationContext{
		DB:     app.DB,
		Spaces: app.SpacesStore,
	}); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	return nil
}

// taskEnv holds the pear components a task can use. pear builds them once, so
// tasks don't construct their own. Add a field when a task needs a component
// that isn't available yet.
type taskEnv struct {
	DB     *gorm.DB
	Spaces spaces.Store
	Perms  perms.Store
	Blobs  spaces.BlobStore
	// Search is nil where search isn't configured (no --meilisearch_url), so
	// tasks that use it must check.
	Search search.Index
}

// taskAction adapts a task to a urfave action: it builds the taskEnv, then
// runs fn with the task's own flags and args available on cmd. Tasks that can
// run more than once must be idempotent or safe to resume, since Cloud Run
// retries failed jobs.
func taskAction(
	fn func(ctx context.Context, cmd *cli.Command, env taskEnv) error,
) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) (err error) {
		ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		// Tasks report traces, metrics and logs like the server, so a task
		// such as a reindex can be followed while it runs.
		otelClose, err := telemetry.SetupOpenTelemetry(ctx, "pear")
		defer func() { _ = otelClose(context.Background()) }()
		if err != nil {
			return fmt.Errorf("setup open telemetry for metric/trace/log collection: %w", err)
		}
		// Operators watch a task run, so it always logs to stdout too.
		slog.SetDefault(log.New(log.WithStdout(true)))
		ctx, span := otel.Tracer("pear/task").Start(ctx, "task "+cmd.Name)
		defer func() { telemetry.End(span, err) }()

		app, err := setupComponents(ctx, cmd)
		if err != nil {
			return err
		}
		defer func() { _ = app.Close() }()
		return fn(ctx, cmd, taskEnv{
			DB:     app.DB,
			Spaces: app.SpacesStore,
			Perms:  app.PermStore,
			Blobs:  app.BlobStore,
			Search: app.SearchIndex,
		})
	}
}
