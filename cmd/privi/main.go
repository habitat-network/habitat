package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/eagraf/habitat-new/internal/node/logging"
	"github.com/eagraf/habitat-new/internal/privi"
)

func main() {
	logger := logging.NewLogger()
	nodeConfig, err := config.NewNodeConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("error loading node config")
	}
	// Create database file if it does not exist
	// TODO: this should really be taken in as an argument or env variable
	priviRepoPath := nodeConfig.PriviRepoFile()
	_, err = os.Stat(priviRepoPath)
	if errors.Is(err, os.ErrNotExist) {
		_, err := os.Create(priviRepoPath)
		if err != nil {
			logger.Err(err).Msgf("unable to create privi repo file at %s", priviRepoPath)
		}
	} else if err != nil {
		logger.Err(err).Msgf("error finding privi repo file")
	}

	priviDB, err := sql.Open("sqlite3", priviRepoPath)
	if err != nil {
		logger.Fatal().Err(err).Msg("unable to open sqlite file backing privi server")
	}

	_, err = priviDB.Exec(privi.CreateTableSQL())
	if err != nil {
		logger.Fatal().Err(err).Msg("unable to setup privi sqlite db")
	}

	priviServer := privi.NewServer(nil, privi.NewSQLiteRepo(priviDB))

	mux := http.NewServeMux()
	mux.HandleFunc("/xrpc/com.habitat.putRecord", priviServer.PutRecord)
	mux.HandleFunc(
		"/xrpc/com.habitat.getRecord",
		priviServer.PdsAuthMiddleware(priviServer.GetRecord),
	)
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
      "id": "#privi",
      "serviceEndpoint": "https://%s",
      "type": "PriviServer"
    }
  ]
}`
		// TODO: this should really be taken in as an argument or env variable
		domain := nodeConfig.Domain()
		_, err := w.Write([]byte(fmt.Sprintf(template, domain, domain)))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	logger.Info().Msg("starting privi server")
	err = http.ListenAndServe(":8080", mux)
	if err != nil {
		logger.Fatal().Err(err).Msg("error starting privi server")
	}
}
