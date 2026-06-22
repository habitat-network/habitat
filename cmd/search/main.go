package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/urfave/cli/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	cmd := &cli.Command{
		Name:   "search",
		Usage:  "Habitat Search Server",
		Flags:  getFlags(),
		Action: run,
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatalf("Error running command: %v", err)
	}
}

func run(_ context.Context, cmd *cli.Command) error {
	db, err := gorm.Open(postgres.Open(cmd.String(fDB)), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("failed to open db: %w", err)
	}

	index, err := newPostgresFTSIndex(db)
	if err != nil {
		return fmt.Errorf("failed to set up index: %w", err)
	}

	client := newPearClient(cmd.String(fPearHost), cmd.String(fM2MToken))

	indexer, err := NewIndexer(db, index, client)
	if err != nil {
		return fmt.Errorf("failed to set up indexer: %w", err)
	}

	indexerCtx, stopIndexer := context.WithCancel(context.Background())
	defer stopIndexer()
	go func() {
		if err := indexer.Run(indexerCtx); err != nil {
			slog.ErrorContext(indexerCtx, "indexer stopped", "err", err)
		}
	}()

	server := NewServer(index, client)
	router := mux.NewRouter()
	router.HandleFunc("/xrpc/network.habitat.search.query", server.HandleQuery).Methods("GET")

	port := cmd.Int(fPort)
	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{Addr: addr, Handler: router}

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("Starting search server on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-sigCtx.Done()
	log.Println("Shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
