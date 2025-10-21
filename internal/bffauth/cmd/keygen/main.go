package main

import (
	"fmt"
	"log"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
)

func main() {
	// Use indigo's crypto package to generate a P-256 key pair
	privateKey, err := atcrypto.GeneratePrivateKeyP256()
	if err != nil {
		log.Fatalf("failed to generate key: %v", err)
	}

	publicKey, err := privateKey.PublicKey()
	if err != nil {
		log.Fatalf("failed to generate public key: %v", err)
	}

	fmt.Printf("Private key multibase: %s\n", privateKey.Multibase())
	fmt.Printf("Public key multibase: %s\n", publicKey.Multibase())
}
