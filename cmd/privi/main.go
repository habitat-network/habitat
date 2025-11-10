package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"os"

	"github.com/eagraf/habitat-new/internal/permissions"
	"github.com/eagraf/habitat-new/internal/privi"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
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
		log.Fatal().Err(err).Msg("error running command")
	}
}

func run(_ context.Context, cmd *cli.Command) error {
	log.Info().Msgf("running with flags: ")
	for _, flag := range cmd.FlagNames() {
		log.Info().Msgf("%s: %v", flag, cmd.Value(flag))
	}
	dbPath := cmd.String(fDb)
	// Create database file if it does not exist
	_, err := os.Stat(dbPath)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("Privi repo file does not exist; creating...")
		_, err := os.Create(dbPath)
		if err != nil {
			return fmt.Errorf("unable to create privi repo file at %s: %w", dbPath, err)
		}
	} else if err != nil {
		return fmt.Errorf("error finding privi repo file: %w", err)
	}

	priviDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("unable to open sqlite file backing privi server: %w", err)
	}

	repo, err := privi.NewSQLiteRepo(priviDB)
	if err != nil {
		return fmt.Errorf("unable to setup privi repo: %w", err)
	}

	adapter, err := permissions.NewSQLiteStore(priviDB)
	if err != nil {
		return fmt.Errorf("unable to setup permissions store: %w", err)
	}
	priviServer := privi.NewServer(adapter, repo)

	mux := http.NewServeMux()

	loggingMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			x, err := httputil.DumpRequest(r, true)
			if err != nil {
				http.Error(w, fmt.Sprint(err), http.StatusInternalServerError)
				return
			}
			fmt.Println("Got a request: ", string(x))
			next.ServeHTTP(w, r)
		})
	}

	mux.HandleFunc("/xrpc/com.habitat.putRecord", priviServer.PutRecord)
	mux.HandleFunc("/xrpc/com.habitat.getRecord", priviServer.GetRecord)
	mux.HandleFunc("/xrpc/network.habitat.uploadBlob", priviServer.UploadBlob)
	mux.HandleFunc("/xrpc/com.habitat.listPermissions", priviServer.ListPermissions)
	mux.HandleFunc("/xrpc/com.habitat.addPermission", priviServer.AddPermission)
	mux.HandleFunc("/xrpc/com.habitat.removePermission", priviServer.RemovePermission)

	mux.HandleFunc("/.well-known/did.json", func(w http.ResponseWriter, r *http.Request) {
		template := `{
  "id": "did:web:%s",
  "@context": [
    "https://www.w3.org/ns/did/v1",
    "https://w3id.org/security/multikey/v1", 
    "https://w3id.org/security/suites/secp256k1-2019/v1"
  ],
  "service": [
    {
      "id": "#habitat",
      "serviceEndpoint": "https://%s",
      "type": "HabitatServer"
    }
  ]
}`
		domain := cmd.String(fDomain)
		_, err := fmt.Fprintf(w, template, domain, domain)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	port := cmd.String(fPort)
	s := &http.Server{
		Handler: loggingMiddleware(mux),
		Addr:    fmt.Sprintf(":%s", port),
	}

	fmt.Println("Starting server on port :" + port)
	certs := cmd.String(fHttpsCerts)
	if certs == "" {
		return s.ListenAndServe()
	}
	return s.ListenAndServeTLS(
		fmt.Sprintf("%s%s", certs, "fullchain.pem"),
		fmt.Sprintf("%s%s", certs, "privkey.pem"),
	)
}
