package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/habitat-network/habitat/internal/encrypt"
)

func main() {
	// By default emit a 32-byte base64 secret (pds_cred_encrypt_key, oauth_*_secret).
	// -p256 instead emits a multibase-encoded P-256 private key (space_signing_key).
	p256 := flag.Bool(
		"p256",
		false,
		"generate a multibase-encoded P-256 private key instead of a secret",
	)
	flag.Parse()

	if err := run(*p256); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(p256 bool) error {
	if p256 {
		key, err := atcrypto.GeneratePrivateKeyP256()
		if err != nil {
			return err
		}
		fmt.Println(key.Multibase())
		return nil
	}
	key, err := encrypt.GenerateKey()
	if err != nil {
		return err
	}
	fmt.Println(key)
	return nil
}
