package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		Name:   "calendar",
		Usage:  "Habitat Calendar Server - Import Google Calendar events",
		Flags:  getFlags(),
		Action: run,
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatalf("Error running command: %v", err)
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	store, err := NewStore(cmd.String(fDB))
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer store.Close()

	googleClient := NewGoogleClient(
		cmd.String(fGoogleClientID),
		cmd.String(fGoogleClientSecret),
		fmt.Sprintf("https://%s/google/callback", cmd.String(fDomain)),
		store,
	)

	authMethod := authn.NewServiceAuthMethod(identity.DefaultDirectory())

	server := NewServer(
		googleClient,
		store,
		authMethod,
		cmd.String(fDomain),
	)

	port := cmd.Int(fPort)
	addr := fmt.Sprintf(":%d", port)

	srv := &http.Server{
		Addr:    addr,
		Handler: server.Router(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("Starting calendar server on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}

	log.Println("Server stopped")
	return nil
}
