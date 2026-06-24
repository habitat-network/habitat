package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/mux"
	"github.com/habitat-network/habitat/internal/sap"
	"github.com/urfave/cli/v3"
	"golang.org/x/sync/errgroup"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// hardcodedOrgHandle is the single org this prototype indexes. Replace
// with the handle of the org you want to index locally. Multi-org indexing
// is out of scope until search moves to instance-wide indexing.
const hardcodedOrgDID = syntax.DID("did:web:ewzw89.pear.local.habitat.network")

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

func run(ctx context.Context, cmd *cli.Command) error {
	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(strings.ToUpper(cmd.String(fLogLevel)))); err != nil {
		return fmt.Errorf("unmarshal log level: %w", err)
	}
	slog.SetLogLoggerLevel(logLevel)

	db, err := gorm.Open(postgres.Open(cmd.String(fDB)), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("failed to open db: %w", err)
	}

	index, err := newPostgresFTSIndex(db)
	if err != nil {
		return fmt.Errorf("failed to set up index: %w", err)
	}

	s, err := sap.NewSap(sap.SapConfig{
		DB:           db,
		PublicDomain: cmd.String(fDomain),
		Secret:       cmd.String(fSecret),
	})
	if err != nil {
		return fmt.Errorf("failed to set up sap: %w", err)
	}

	orgs, err := s.ListOrgs(ctx)
	if err != nil {
		return fmt.Errorf("failed to list orgs: %w", err)
	}
	if !slices.Contains(orgs, hardcodedOrgDID) {
		url, err := s.AddOrg(ctx, hardcodedOrgDID.String())
		if err != nil {
			return fmt.Errorf("failed to add org: %w", err)
		}
		fmt.Printf("add org via url: %s\n", url)
	}

	server := NewServer(cmd.String(fPearHost), index)
	indexer := NewIndexer(index, s)

	router := mux.NewRouter()
	router.HandleFunc("/xrpc/network.habitat.search.query", server.HandleQuery).Methods("GET")
	router.Handle("/oauth-callback", s)
	router.Handle("/client-metadata.json", s)

	port := cmd.Int(fPort)
	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{Addr: addr, Handler: router}

	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	eg, egCtx := errgroup.WithContext(sigCtx)
	eg.Go(func() error {
		return s.Start(egCtx)
	})
	eg.Go(func() error {
		return indexer.Run(egCtx)
	})
	eg.Go(func() error {
		log.Printf("Starting search server on %s", addr)
		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.ErrorContext(egCtx, "server error", "err", err)
			}
		}()
		<-egCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	})

	return eg.Wait()
}
