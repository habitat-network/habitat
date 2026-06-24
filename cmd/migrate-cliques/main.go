package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/rs/zerolog"
	"github.com/urfave/cli/v3"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/clique"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/spaces"
)

func main() {
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "pg-url",
				Usage: "Postgres connection URL",
			},
			&cli.StringFlag{
				Name:  "db",
				Usage: "SQLite database path",
			},
		},
		Action: run,
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	pgURL := cmd.String("pg-url")
	dbPath := cmd.String("db")
	if pgURL == "" && dbPath == "" {
		return fmt.Errorf("either --pg-url or --db is required")
	}

	var db *gorm.DB
	var err error
	if pgURL != "" {
		db, err = gorm.Open(postgres.Open(pgURL), &gorm.Config{})
	} else {
		db, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	}
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	var fga fgastore.Store
	if pgURL != "" {
		fga, err = fgastore.NewPostgres(ctx, pgURL)
	} else {
		fga, err = fgastore.NewSQLite(ctx, dbPath)
	}
	if err != nil {
		return fmt.Errorf("setup fga: %w", err)
	}
	defer fga.Close()

	cliqueStore, err := clique.NewStore(db)
	if err != nil {
		return fmt.Errorf("setup clique store: %w", err)
	}

	spacesStore, err := spaces.NewStore(db, fga)
	if err != nil {
		return fmt.Errorf("setup spaces store: %w", err)
	}

	count, err := MigrateCliques(ctx, db, cliqueStore, spacesStore)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	log.Printf("migration complete: %d cliques migrated to spaces", count)
	return nil
}
