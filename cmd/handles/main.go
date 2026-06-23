package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/habitat-network/habitat/internal/handles"
	"github.com/urfave/cli/v3"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func main() {
	flags, mutuallyExclusiveFlags := getFlags()
	cmd := &cli.Command{
		Flags:                  flags,
		MutuallyExclusiveFlags: mutuallyExclusiveFlags,
		Action:                 run,
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(_ context.Context, cmd *cli.Command) error {
	db := setupDB(cmd)
	h, err := handles.New(db)
	if err != nil {
		return fmt.Errorf("failed to create hive: %w", err)
	}

	server, err := handles.NewServer(h)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/atproto-did", server.ServeHandle)
	mux.HandleFunc("/xrpc/network.habitat.handle.register", server.MintHandle)

	err = http.ListenAndServe(":"+cmd.String(fPort), mux)
	if err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}
	return nil
}

func setupDB(cmd *cli.Command) *gorm.DB {
	var db *gorm.DB
	var err error

	postgresUrl := cmd.String(fPgUrl)
	if postgresUrl != "" {
		db, err = gorm.Open(postgres.Open(postgresUrl), &gorm.Config{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to open postgres database: %v\n", err)
			os.Exit(1)
		}
	} else {
		dbPath := cmd.String(fDb)
		db, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to open sqlite database: %v\n", err)
			os.Exit(1)
		}
	}
	return db
}
