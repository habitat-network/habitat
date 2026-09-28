// Command jwttool is a manual debugging helper for minting the JWTs defined
// in internal/utils (ServiceAuthToken, SpaceCredential, DelegationToken).
// The signing key is a multibase-encoded private key parsable by
// atcrypto.ParsePrivateMultibase (as emitted by `keygen -p256`).
//
// Usage:
//
//	jwttool service-auth --key <multibase> --iss <did> --aud <aud> [--lxm <nsid>] [--ttl <duration>]
//	jwttool space-credential --key <multibase> [--kid <kid>] --space <space-uri>
//	jwttool delegation --key <multibase> --iss <did> [--kid <kid>] --space <space-uri>
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	_ "github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "service-auth":
		err = runServiceAuth(os.Args[2:])
	case "space-credential":
		err = runSpaceCredential(os.Args[2:])
	case "delegation":
		err = runDelegation(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func parsePrivateKey(multibase string) (atcrypto.PrivateKey, error) {
	if multibase == "" {
		return nil, fmt.Errorf("--key is required (multibase-encoded private key)")
	}
	key, err := atcrypto.ParsePrivateMultibase(multibase)
	if err != nil {
		return nil, fmt.Errorf("parse --key as multibase private key: %w", err)
	}
	return key, nil
}

func runServiceAuth(args []string) error {
	fs := flag.NewFlagSet("service-auth", flag.ContinueOnError)
	keyStr := fs.String(
		"key",
		"",
		"multibase-encoded private key (parsable by atcrypto.ParsePrivateMultibase)",
	)
	issStr := fs.String("iss", "", "issuer DID (e.g. did:web:example.com)")
	aud := fs.String("aud", "", "audience (e.g. did:web:peer.example.com or host)")
	lxmStr := fs.String("lxm", "", "lexicon method NSID (optional)")
	ttlStr := fs.String("ttl", "", "TTL as Go duration (optional, default 60s, max 30m)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *issStr == "" {
		return fmt.Errorf("--iss is required")
	}
	if *aud == "" {
		return fmt.Errorf("--aud is required")
	}

	key, err := parsePrivateKey(*keyStr)
	if err != nil {
		return err
	}
	iss, err := syntax.ParseDID(*issStr)
	if err != nil {
		return fmt.Errorf("parse --iss as DID: %w", err)
	}
	var lxm *syntax.NSID
	if *lxmStr != "" {
		parsed, err := syntax.ParseNSID(*lxmStr)
		if err != nil {
			return fmt.Errorf("parse --lxm as NSID: %w", err)
		}
		lxm = &parsed
	}
	var ttl *time.Duration
	if *ttlStr != "" {
		parsed, err := time.ParseDuration(*ttlStr)
		if err != nil {
			return fmt.Errorf("parse --ttl as duration: %w", err)
		}
		ttl = &parsed
	}

	token, err := utils.ServiceAuthToken(key, iss, *aud, lxm, ttl)
	if err != nil {
		return fmt.Errorf("sign service-auth token: %w", err)
	}
	fmt.Println(token)
	return nil
}

func runSpaceCredential(args []string) error {
	fs := flag.NewFlagSet("space-credential", flag.ContinueOnError)
	keyStr := fs.String(
		"key",
		"",
		"multibase-encoded private key (parsable by atcrypto.ParsePrivateMultibase)",
	)
	kid := fs.String("kid", "#atproto", "key ID for the JWT header")
	spaceStr := fs.String(
		"space",
		"",
		"space URI (e.g. at://did:web:example.com/space/community.opensocial.about/self)",
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *spaceStr == "" {
		return fmt.Errorf("--space is required")
	}

	key, err := parsePrivateKey(*keyStr)
	if err != nil {
		return err
	}
	space, err := habitat_syntax.ParseSpaceURI(*spaceStr)
	if err != nil {
		return fmt.Errorf("parse --space as space URI: %w", err)
	}

	token, err := utils.SpaceCredential(key, *kid, space)
	if err != nil {
		return fmt.Errorf("sign space credential: %w", err)
	}
	fmt.Println(token)
	return nil
}

func runDelegation(args []string) error {
	fs := flag.NewFlagSet("delegation", flag.ContinueOnError)
	keyStr := fs.String(
		"key",
		"",
		"multibase-encoded private key (parsable by atcrypto.ParsePrivateMultibase)",
	)
	issStr := fs.String("iss", "", "issuer DID (e.g. did:web:example.com)")
	kid := fs.String("kid", "#atproto", "key ID for the JWT header")
	spaceStr := fs.String(
		"space",
		"",
		"space URI (e.g. at://did:web:example.com/space/community.opensocial.about/self)",
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *issStr == "" {
		return fmt.Errorf("--iss is required")
	}
	if *spaceStr == "" {
		return fmt.Errorf("--space is required")
	}

	key, err := parsePrivateKey(*keyStr)
	if err != nil {
		return err
	}
	iss, err := syntax.ParseDID(*issStr)
	if err != nil {
		return fmt.Errorf("parse --iss as DID: %w", err)
	}
	space, err := habitat_syntax.ParseSpaceURI(*spaceStr)
	if err != nil {
		return fmt.Errorf("parse --space as space URI: %w", err)
	}

	token, err := utils.DelegationToken(key, iss, *kid, space)
	if err != nil {
		return fmt.Errorf("sign delegation token: %w", err)
	}
	fmt.Println(token)
	return nil
}

func usage() {
	fmt.Fprint(
		os.Stderr,
		`jwttool — mint debugging JWTs from internal/utils with a multibase private key

usage:
  jwttool service-auth --key <multibase> --iss <did> --aud <aud> [--lxm <nsid>] [--ttl <duration>]
  jwttool space-credential --key <multibase> [--kid <kid>] --space <space-uri>
  jwttool delegation --key <multibase> --iss <did> [--kid <kid>] --space <space-uri>
`,
	)
}
