package syntax

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// This package is copied from the atproto implementation of https://atproto.com/specs/at-uri-scheme.
// It is copied from the bluesky-social/indigo package.
// It implements a HabitHabitatURI which operates the exact same as an AT URI, but is namespaced via habitat:// rather than at://
// We must namespace records separately from at:// for private data, to avoid rkey collisions on records that could exist on both the public PDS and the
// private-data focused habitat PDS.

// We reuse as many functions as possible from the public syntax package.

const (
	habitatScheme     = "habitat://"
	habitatCliqueNSID = syntax.NSID("network.habitat.clique")
)

var HabitatURIRegex = regexp.MustCompile(`^habitat:\/\/(?P<authority>[a-zA-Z0-9._:%-]+)(\/(?P<collection>[a-zA-Z0-9-.]+)(\/(?P<rkey>[a-zA-Z0-9_~.:-]{1,512}))?)?$`)

// String type which represents a syntaxtually valid AT URI, as would pass Lexicon syntax validation for the 'at-uri' field (no query or fragment parts)
//
// Always use [ParseHabitatURI] instead of wrapping strings directly, especially when working with input.
//
// Syntax specification: https://atproto.com/specs/at-uri-scheme
type HabitatURI string

func ConstructHabitatUri(did string, collection string, rkey string) HabitatURI {
	sb := strings.Builder{}
	sb.WriteString(habitatScheme)
	sb.WriteString(did)
	sb.WriteString("/")
	sb.WriteString(collection)
	sb.WriteString("/")
	sb.WriteString(rkey)
	return HabitatURI(sb.String())
}

func (uri HabitatURI) ExtractParts() (syntax.DID, syntax.NSID, syntax.RecordKey, error) {
	did, err := uri.Authority().AsDID()
	if err != nil {
		return "", "", "", err
	}
	return did, uri.Collection(), uri.RecordKey(), nil
}

func ParseHabitatURI(raw string) (HabitatURI, error) {
	if len(raw) > 8192 {
		return "", errors.New("HabitatURI is too long (8192 chars max)")
	}
	parts := HabitatURIRegex.FindStringSubmatch(raw)
	if len(parts) < 2 || parts[0] == "" {
		return "", errors.New("AT-URI syntax didn't validate via regex")
	}
	// verify authority as either a DID or NSID
	_, err := syntax.ParseAtIdentifier(parts[1])
	if err != nil {
		return "", fmt.Errorf("AT-URI authority section neither a DID nor Handle: %s", parts[1])
	}
	if len(parts) >= 4 && parts[3] != "" {
		_, err := syntax.ParseNSID(parts[3])
		if err != nil {
			return "", fmt.Errorf("AT-URI first path segment not an NSID: %s", parts[3])
		}
	}
	if len(parts) >= 6 && parts[5] != "" {
		_, err := syntax.ParseRecordKey(parts[5])
		if err != nil {
			return "", fmt.Errorf("AT-URI second path segment not a RecordKey: %s", parts[5])
		}
	}
	return HabitatURI(raw), nil
}

func ParseHabitatClique(raw string) (HabitatURI, error) {
	uri, err := ParseHabitatURI(raw)
	if err != nil {
		return "", err
	}

	if uri.Collection() != habitatCliqueNSID {
		return "", fmt.Errorf("input does not use clique nsid: %s, raw")
	}
	return uri, nil
}

// Every valid HabitatURI has a valid AtIdentifier in the authority position.
//
// If this HabitatURI is malformed, returns empty
func (n HabitatURI) Authority() syntax.AtIdentifier {
	parts := strings.SplitN(string(n), "/", 4)
	if len(parts) < 3 {
		// something has gone wrong (would not validate)
		return syntax.AtIdentifier{}
	}
	atid, err := syntax.ParseAtIdentifier(parts[2])
	if err != nil {
		return syntax.AtIdentifier{}
	}
	return *atid
}

// Returns path segment, without leading slash, as would be used in an atproto repository key. Or empty string if there is no path.
func (n HabitatURI) Path() string {
	parts := strings.SplitN(string(n), "/", 5)
	if len(parts) < 4 {
		// something has gone wrong (would not validate)
		return ""
	}
	if len(parts) == 4 {
		return parts[3]
	}
	return parts[3] + "/" + parts[4]
}

// Returns a valid NSID if there is one in the appropriate part of the path, otherwise empty.
func (n HabitatURI) Collection() syntax.NSID {
	parts := strings.SplitN(string(n), "/", 5)
	if len(parts) < 4 {
		// something has gone wrong (would not validate)
		return syntax.NSID("")
	}
	nsid, err := syntax.ParseNSID(parts[3])
	if err != nil {
		return syntax.NSID("")
	}
	return nsid
}

func (n HabitatURI) RecordKey() syntax.RecordKey {
	parts := strings.SplitN(string(n), "/", 6)
	if len(parts) < 5 {
		// something has gone wrong (would not validate)
		return syntax.RecordKey("")
	}
	rkey, err := syntax.ParseRecordKey(parts[4])
	if err != nil {
		return syntax.RecordKey("")
	}
	return rkey
}

func (n HabitatURI) Normalize() HabitatURI {
	auth := n.Authority()
	if auth.Inner == nil {
		// invalid AT-URI; return the current value (!)
		return n
	}
	coll := n.Collection()
	if coll == syntax.NSID("") {
		return HabitatURI(habitatScheme + auth.Normalize().String())
	}
	rkey := n.RecordKey()
	if rkey == syntax.RecordKey("") {
		return HabitatURI(habitatScheme + auth.Normalize().String() + "/" + coll.String())
	}
	return HabitatURI(habitatScheme + auth.Normalize().String() + "/" + coll.Normalize().String() + "/" + rkey.String())
}

func (n HabitatURI) String() string {
	return string(n)
}

func (a HabitatURI) MarshalText() ([]byte, error) {
	return []byte(a.String()), nil
}

func (a *HabitatURI) UnmarshalText(text []byte) error {
	HabitatURI, err := ParseHabitatURI(string(text))
	if err != nil {
		return err
	}
	*a = HabitatURI
	return nil
}
