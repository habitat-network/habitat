package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/cmd/pear/migrations"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/spaces"
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

	c, err := setupComponents(ctx, cmd)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if err := migrations.Run(ctx, migrations.PearMigrationContext{
		DB:     c.database,
		Spaces: c.spacesStore,
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
	// Search is nil where search isn't configured (no --meilisearch_url), so
	// tasks that use it must check.
	Search search.Index
}

// taskAction adapts a task to a urfave action: it builds the taskEnv, then
// runs fn with the task's own flags and args available on cmd. Tasks that can
// run more than once must be idempotent or safe to resume, since Cloud Run
// retries failed jobs.
//
//nolint:unused // used by tasks as they are added to taskCommands
func taskAction(
	fn func(ctx context.Context, cmd *cli.Command, env taskEnv) error,
) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		c, err := setupComponents(ctx, cmd)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()
		return fn(ctx, cmd, taskEnv{
			DB:     c.database,
			Spaces: c.spacesStore,
			Search: c.searchIndex,
		})
	}
}
