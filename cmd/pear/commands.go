package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/cmd/pear/migrations"
	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/spaces"
)

func commands() []*cli.Command {
	return []*cli.Command{
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

// setupCommandComponents builds the database and spaces layer for the migrate
// and run commands, which share the server's configuration but don't serve.
func setupCommandComponents(
	ctx context.Context,
	cmd *cli.Command,
) (*gorm.DB, *spacesComponents, error) {
	database, err := db.New(cmd.String(fDB))
	if err != nil {
		return nil, nil, fmt.Errorf("setup database: %w", err)
	}
	domain := cmd.String(fDomain)
	hiveDomain := cmd.String(fHiveDomain)
	if hiveDomain == "" {
		hiveDomain = domain
	}
	h, err := hive.NewHive(hiveDomain, domain, database.WithContext(ctx))
	if err != nil {
		return nil, nil, fmt.Errorf("setup hive: %w", err)
	}
	sp, err := setupSpaces(ctx, cmd, database, h)
	if err != nil {
		return nil, nil, err
	}
	return database, sp, nil
}

func migrate(ctx context.Context, cmd *cli.Command) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	database, sp, err := setupCommandComponents(ctx, cmd)
	if err != nil {
		return err
	}
	defer func() { _ = sp.Close() }()
	if err := migrations.Run(ctx, migrations.PearMigrationContext{
		DB:     database,
		Spaces: sp.spacesStore,
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

		database, sp, err := setupCommandComponents(ctx, cmd)
		if err != nil {
			return err
		}
		defer func() { _ = sp.Close() }()
		return fn(ctx, cmd, taskEnv{
			DB:     database,
			Spaces: sp.spacesStore,
			Search: sp.searchIndex,
		})
	}
}
