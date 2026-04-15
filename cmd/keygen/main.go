package main

import (
	"fmt"
	"os"

	"github.com/habitat-network/habitat/internal/encrypt"
)

func main() {
	key, err := encrypt.GenerateKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(key)
}
