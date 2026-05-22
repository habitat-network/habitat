package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
	"tailscale.com/tsnet"
)

func main() {
	flags := getFlags()
	cmd := &cli.Command{
		Flags:  flags,
		Action: run,
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal().Err(err).Msg("error running command")
	}
}

func run(_ context.Context, cmd *cli.Command) error {
	callbackURL := "https://" + cmd.String(fPearDomain) + "/oauth-callback"
	secretBytes, err := encrypt.ParseKey(cmd.String(fSecret))
	if err != nil {
		return fmt.Errorf("failed to decode secret: %w", err)
	}
	priv, err := atcrypto.ParsePrivateBytesP256(secretBytes)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}
	server := &tsnet.Server{
		Hostname: cmd.String(fMachineName),
		Dir:      ".habitat/ocm",
	}
	ln, err := server.ListenFunnel("tcp", ":443")
	if err != nil {
		log.Fatal().Err(err).Msg("unable to listen")
	}
	return http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		clientUri := "https://" + r.Host + r.URL.String()
		config := oauth.NewPublicConfig(
			clientUri,
			callbackURL,
			[]string{"atproto", "transition:generic"},
		)
		if err := config.SetClientSecret(priv, "habitat"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		metadata := config.ClientMetadata()
		metadata.ClientName = new("Habitat")
		metadata.ClientURI = new(clientUri)
		jwks := config.PublicJWKS()
		metadata.JWKS = &jwks
		if err := json.NewEncoder(w).Encode(metadata); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
}
