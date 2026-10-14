package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"

	"github.com/urfave/cli/v3"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/cmd/pear/migrations"
	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/tasks"
)

const fList = "list"

func commands() []*cli.Command {
	return []*cli.Command{
		{
			Name:   "migrate",
			Usage:  "Apply pending database migrations and exit",
			Action: migrate,
		},
		{
			Name:      "run",
			Usage:     "Run a registered task (backfills and other operator-run work)",
			ArgsUsage: "<task> [task args...]",
			Flags: []cli.Flag{
				&cli.BoolFlag{Name: fList, Usage: "List the registered tasks and exit"},
			},
			Action: runTask,
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

func runTask(ctx context.Context, cmd *cli.Command) error {
	registry := tasks.All()
	if cmd.Bool(fList) {
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		for _, t := range registry.List() {
			_, _ = fmt.Fprintf(w, "%s\t%s\n", t.Name, t.Description)
		}
		return w.Flush()
	}
	if cmd.Args().Len() == 0 {
		return errors.New("usage: pear run <task> [args...] (see pear run --list)")
	}
	name := tasks.Name(cmd.Args().First())
	args := cmd.Args().Tail()

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	database, sp, err := setupCommandComponents(ctx, cmd)
	if err != nil {
		return err
	}
	defer func() { _ = sp.Close() }()
	return registry.Run(ctx, name, tasks.Env{
		DB:     database,
		Spaces: sp.spacesStore,
		Search: sp.searchIndex,
	}, args)
}
