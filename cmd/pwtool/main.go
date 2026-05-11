// Command pwtool is a manual debugging helper for the argon2id password
// hashing used by internal/org. It uses the same argon2id wrapper and
// parameters as hashPassword/verifyPassword so hashes are interchangeable.
//
// Usage:
//
//	pwtool hash "plaintext"
//	pwtool verify --pass "plaintext" --hash "$argon2id$..."
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/alexedwards/argon2id"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "hash":
		if err := runHash(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "verify":
		if err := runVerify(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

func runHash(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("hash requires exactly one positional arg: the plaintext password")
	}
	out, err := argon2id.CreateHash(args[0], argon2id.DefaultParams)
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

func runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	pass := fs.String("pass", "", "plaintext password to check")
	hash := fs.String("hash", "", "encoded argon2id hash to check against")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pass == "" || *hash == "" {
		return fmt.Errorf("--pass and --hash are both required")
	}
	ok, err := argon2id.ComparePasswordAndHash(*pass, *hash)
	if err != nil {
		return fmt.Errorf("malformed hash: %w", err)
	}
	if ok {
		fmt.Println("match")
		return nil
	}
	fmt.Println("no match")
	os.Exit(1)
	return nil
}

func usage() {
	fmt.Fprint(os.Stderr, `pwtool — manual argon2id hash/verify for internal/org passwords

usage:
  pwtool hash "plaintext"
  pwtool verify --pass "plaintext" --hash "$argon2id$..."
`)
}
