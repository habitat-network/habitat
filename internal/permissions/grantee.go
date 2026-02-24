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

	CliqueNSID          = syntax.NSID("network.habitat.clique")
	FollowersCliqueRkey = syntax.RecordKey("followers")
)

type Grantee interface {
	isGrantee()
	String() string
}

type CliqueGrantee habitat_syntax.HabitatURI

var _ Grantee = CliqueGrantee("")

func (g CliqueGrantee) isGrantee() {}
func (g CliqueGrantee) String() string {
	return string(g)
}
func (g CliqueGrantee) Owner() syntax.DID {
	return syntax.DID(habitat_syntax.HabitatURI(g).Authority().String())
}

func (g CliqueGrantee) RecordKey() syntax.RecordKey {
	return habitat_syntax.HabitatURI(g).RecordKey()
}

type DIDGrantee syntax.DID

var _ Grantee = DIDGrantee("")

func (g DIDGrantee) isGrantee() {}
func (g DIDGrantee) String() string {
	return string(g)
}

// Try to parse the string as either a clique or did grantee.
func ParseGranteeFromString(grantee string) (Grantee, error) {
	did, err := syntax.ParseDID(grantee)
	if err == nil {
		return DIDGrantee(did), nil
	}
	clique, err := parseHabitatClique(grantee)
	if err == nil {
		return CliqueGrantee(clique), nil
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

		var asStr string
		switch granteeType {
		case "network.habitat.grantee#didGrantee":
			did, ok := unknownGrantee["did"]
			if !ok {
				return nil, fmt.Errorf(
					"malformatted did grantee has no did field: %v",
					unknownGrantee,
				)
			}
			asStr, ok = did.(string)
			if !ok {
				return nil, fmt.Errorf(
					"malformatted did grantee has non-string did field: %v",
					unknownGrantee,
				)
			}
		case "network.habitat.grantee#cliqueRef":
			uri, ok := unknownGrantee["uri"]
			if !ok {
				return nil, fmt.Errorf(
					"malformatted clique grantee has no uri field: %v",
					unknownGrantee,
				)
			}
			asStr, ok = uri.(string)
			if !ok {
				return nil, fmt.Errorf(
					"malformatted clique grantee has non-string uri field: %v",
					unknownGrantee,
				)
			}
		default:
			return nil, fmt.Errorf(
				"malformatted grantee has unknown $type of %v: %v",
				granteeType,
				unknownGrantee,
			)
		}
		grantee, err := ParseGranteeFromString(asStr)
		if err != nil {
			return nil, err
		}
		parsed[i] = grantee
	}
	return parsed, nil
}

func parseHabitatClique(raw string) (habitat_syntax.HabitatURI, error) {
	uri, err := habitat_syntax.ParseHabitatURI(raw)
	if err != nil {
		return "", err
	}

	if uri.Collection() != CliqueNSID {
		return "", fmt.Errorf("input does not use clique nsid: %s", raw)
	}
	return uri, nil
}
