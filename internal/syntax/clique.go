package syntax

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

const (
	ReservedCliqueNSID = "network.habitat.clique"
)

var CliqueRefRegex = regexp.MustCompile(`^clique:(?P<authority>[a-zA-Z0-9._:%-]+)(\/(?P<key>[a-zA-Z0-9-.]+))$`)

func ConstructClique(owner syntax.DID, key string) Clique {
	return Clique(fmt.Sprintf("clique:%s/%s", owner, key))
}

// A string that matches CliqueRefRegex
type Clique string

// Clique implements Grantee.
func (c Clique) IsGrantee() {}

func ParseClique(raw string) (Clique, error) {
	if len(raw) > 8192 {
		return "", errors.New("Clique is too long (8192 chars max)")
	}
	parts := CliqueRefRegex.FindStringSubmatch(raw)
	if len(parts) < 2 || parts[0] == "" {
		return "", errors.New("Clique syntax didn't validate via regex")
	}
	// authority must be a valid DID
	_, err := syntax.ParseDID(parts[1])
	if err != nil {
		return "", fmt.Errorf("Clique authority is not a valid DID: %s", parts[1])
	}
	return Clique(raw), nil
}

// Authority returns the DID owner of the clique, or empty string if malformed.
func (c Clique) Authority() syntax.DID {
	s := strings.TrimPrefix(string(c), "clique:")
	parts := strings.SplitN(s, "/", 2)
	did, err := syntax.ParseDID(parts[0])
	if err != nil {
		return ""
	}
	return did
}

// Key returns the key segment of the clique, or empty string if malformed.
func (c Clique) Key() string {
	s := strings.TrimPrefix(string(c), "clique:")
	parts := strings.SplitN(s, "/", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func (c Clique) String() string {
	return string(c)
}

func (c Clique) MarshalText() ([]byte, error) {
	return []byte(c.String()), nil
}

func (c *Clique) UnmarshalText(text []byte) error {
	clique, err := ParseClique(string(text))
	if err != nil {
		return err
	}
	*c = clique
	return nil
}
