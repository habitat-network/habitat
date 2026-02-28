package main

import "github.com/habitat-network/habitat/internal/encrypt"

func main() {
	println(encrypt.GenerateKey())
}
