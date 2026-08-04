package main

import (
	"fmt"
	"os"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
)

func main() {
	priv, err := atcrypto.GeneratePrivateKeyP256()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(priv.Multibase())
}
