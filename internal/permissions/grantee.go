package permissions

import (
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type GranteeType string

const (
	CliqueType GranteeType = "clique"
	DIDType    GranteeType = "did"

	FollowersCliqueKey = "followers"
)

type Grantee interface {
	IsGrantee()
	String() string
}

type DIDGrantee syntax.DID

var _ Grantee = DIDGrantee("")

func (g DIDGrantee) IsGrantee() {}
func (g DIDGrantee) String() string {
	return string(g)
}

var _ Grantee = habitat_syntax.Clique("")

// Try to parse the string as either a clique or did grantee.
func ParseGranteeFromString(grantee string) (Grantee, error) {
	did, err := syntax.ParseDID(grantee)
	if err == nil {
		return DIDGrantee(did), nil
	}
	clique, err := habitat_syntax.ParseClique(grantee)
	if err == nil {
		return clique, nil
	}

	return nil, fmt.Errorf("unable to parse given string as a valid permission grantee type: %s", grantee)
}

// Parse the grantees input which is typed as an interface
func ParseGranteesFromInterface(grantees []interface{}) ([]Grantee, error) {
	// Tiny optimization to avoid unnecessary allocations
	if len(grantees) == 0 {
		return nil, nil
	}

	parsed := make([]Grantee, len(grantees))
	for i, generic := range grantees {
		unknownGrantee, ok := generic.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected type in grantees field: %v", generic)
		}

		granteeType, ok := unknownGrantee["$type"]
		if !ok {
			return nil, fmt.Errorf("malformatted grantee has no $type field: %v", unknownGrantee)
		}

		var grantee Grantee
		switch granteeType {
		case "network.habitat.grantee#didGrantee":
			maybeDID, ok := unknownGrantee["did"]
			if !ok {
				return nil, fmt.Errorf(
					"malformatted did grantee has no did field: %v",
					unknownGrantee,
				)
			}
			asStr, ok := maybeDID.(string)
			if !ok {
				return nil, fmt.Errorf(
					"malformatted did grantee has non-string did field: %v",
					unknownGrantee,
				)
			}
			did, err := syntax.ParseDID(asStr)
			if err != nil {
				return nil, fmt.Errorf("error parsing did grantee: %w", err)
			}
			grantee = DIDGrantee(did)
		case "network.habitat.grantee#clique":
			var err error
			maybeClique, ok := unknownGrantee["clique"]
			if !ok {
				return nil, fmt.Errorf(
					"malformatted clique grantee has no uri field: %v",
					unknownGrantee,
				)
			}
			asStr, ok := maybeClique.(string)
			if !ok {
				return nil, fmt.Errorf(
					"malformatted clique grantee has non-string uri field: %v",
					unknownGrantee,
				)
			}
			grantee, err = habitat_syntax.ParseClique(asStr)
			if err != nil {
				return nil, fmt.Errorf("error parsing clique grantee (v1): %w", err)
			}
		default:
			return nil, fmt.Errorf(
				"malformatted grantee has unknown $type of %v: %v",
				granteeType,
				unknownGrantee,
			)
		}
		parsed[i] = grantee
	}
	return parsed, nil
}

func ConstructInterfaceFromGrantees(grantees []Grantee) []interface{} {
	// Tiny optimization to avoid unnecessary allocations
	if len(grantees) == 0 {
		return nil
	}

	constructed := make([]any, len(grantees))
	for i, grantee := range grantees {
		switch g := grantee.(type) {
		case DIDGrantee:
			didGrantee := map[string]any{
				"$type": "network.habitat.grantee#didGrantee",
				"did":   g.String(),
			}
			constructed[i] = didGrantee
		case habitat_syntax.Clique:
			clique := map[string]any{
				"$type":  "network.habitat.grantee#clique",
				"clique": g.String(),
			}
			constructed[i] = clique
		}
	}
	return constructed
}
