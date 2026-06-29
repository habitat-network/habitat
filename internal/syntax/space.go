package syntax

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// ReservedRelationshipTupleNSID is the collection for relationship tuple
// records. Like network.habitat.clique, it is managed exclusively through its
// dedicated XRPC endpoints (network.habitat.relationship.*) and must not be
// writable via the generic record-write path, so the FGA graph and the AT
// Protocol records it mirrors stay in sync.
const ReservedRelationshipTupleNSID = "network.habitat.relationship.tuple"

type SpaceKey string

func NewSkey(tid syntax.TID) SpaceKey {
	return SpaceKey(tid)
}

func (s SpaceKey) String() string {
	return string(s)
}

func ParseSkey(s string) (SpaceKey, error) {
	_, err := syntax.ParseRecordKey(s)
	if err != nil {
		return "", err
	}
	return SpaceKey(s), nil
}

// SpaceURI identifies a space.
// Format: "ats://spaceDID/spaceType/skey"
type SpaceURI string

var spaceURIRegex = regexp.MustCompile(
	`^ats:\/\/(?P<did>[a-zA-Z0-9._:%-]+)\/(?P<type>[a-zA-Z0-9-.]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})$`,
)

func ConstructSpaceURI(spaceDID syntax.DID, spaceType syntax.NSID, skey SpaceKey) SpaceURI {
	return SpaceURI(fmt.Sprintf("ats://%s/%s/%s", spaceDID, spaceType, skey))
}

func ParseSpaceURI(raw string) (SpaceURI, error) {
	if len(raw) > 8192 {
		return "", errors.New("SpaceURI is too long (8192 chars max)")
	}
	parts := spaceURIRegex.FindStringSubmatch(raw)
	if len(parts) < 4 || parts[0] == "" {
		return "", errors.New("invalid space URI format")
	}
	_, err := syntax.ParseDID(parts[1])
	if err != nil {
		return "", fmt.Errorf("space URI DID is not valid: %s", parts[1])
	}
	_, err = syntax.ParseNSID(parts[2])
	if err != nil {
		return "", fmt.Errorf("space URI type is not a valid NSID: %s", parts[2])
	}
	return SpaceURI(raw), nil
}

func (s SpaceURI) SpaceOwner() syntax.DID {
	parts := spaceURIRegex.FindStringSubmatch(string(s))
	if len(parts) < 4 {
		return ""
	}
	did, err := syntax.ParseDID(parts[1])
	if err != nil {
		return ""
	}
	return did
}

func (s SpaceURI) SpaceType() syntax.NSID {
	parts := spaceURIRegex.FindStringSubmatch(string(s))
	if len(parts) < 4 {
		return ""
	}
	nsid, err := syntax.ParseNSID(parts[2])
	if err != nil {
		return ""
	}
	return nsid
}

func (s SpaceURI) Skey() SpaceKey {
	parts := spaceURIRegex.FindStringSubmatch(string(s))
	if len(parts) < 4 {
		return ""
	}
	return SpaceKey(parts[3])
}

func (s SpaceURI) String() string {
	return string(s)
}

type SpaceRecordURI string

var spaceRecordURIRegex = regexp.MustCompile(
	`^ats:\/\/[a-zA-Z0-9._:%-]+\/[a-zA-Z0-9-.]+\/[a-zA-Z0-9_~.:-]{1,512}` +
		`\/(?P<repo>[a-zA-Z0-9._:%-]+)\/(?P<collection>[a-zA-Z0-9-.]+)\/(?P<rkey>[a-zA-Z0-9_~.:-]{1,512})$`,
)

var spaceRecordURIPartsRegex = regexp.MustCompile(
	`^ats:\/\/(?P<did>[a-zA-Z0-9._:%-]+)\/(?P<type>[a-zA-Z0-9-.]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})` +
		`\/(?P<repo>[a-zA-Z0-9._:%-]+)\/(?P<collection>[a-zA-Z0-9-.]+)\/(?P<rkey>[a-zA-Z0-9_~.:-]{1,512})$`,
)

func ConstructSpaceRecordURI(
	spaceUri SpaceURI,
	repo syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) SpaceRecordURI {
	return SpaceRecordURI(fmt.Sprintf("%s/%s/%s/%s", spaceUri, repo, collection, rkey))
}

func (s SpaceRecordURI) String() string {
	return string(s)
}

// Collection extracts the NSID of the record's collection from the URI,
// i.e. "{spaceURI}/{repo}/{collection}/{rkey}" -> {collection}. Returns ""
// if the URI doesn't match the expected format.
func (s SpaceRecordURI) Collection() syntax.NSID {
	parts := spaceRecordURIRegex.FindStringSubmatch(string(s))
	if len(parts) < 4 {
		return ""
	}
	nsid, err := syntax.ParseNSID(parts[2])
	if err != nil {
		return ""
	}
	return nsid
}

// SpaceURI extracts the SpaceURI prefix of a SpaceRecordURI, i.e.
// "{spaceURI}/{repo}/{collection}/{rkey}" -> {spaceURI}. Returns "" if the
// URI doesn't match the expected format.
func (s SpaceRecordURI) SpaceURI() SpaceURI {
	parts := spaceRecordURIPartsRegex.FindStringSubmatch(string(s))
	if len(parts) < 7 {
		return ""
	}
	spaceURI, err := ParseSpaceURI(fmt.Sprintf("ats://%s/%s/%s", parts[1], parts[2], parts[3]))
	if err != nil {
		return ""
	}
	return spaceURI
}

// SpaceOwner extracts the DID of the owning space's owner from a
// SpaceRecordURI, equivalent to s.SpaceURI().SpaceOwner(). Returns "" if
// the URI doesn't match the expected format.
func (s SpaceRecordURI) SpaceOwner() syntax.DID {
	return s.SpaceURI().SpaceOwner()
}

// Repo extracts the DID of the repo that owns the record from the URI,
// i.e. "{spaceURI}/{repo}/{collection}/{rkey}" -> {repo}. Returns "" if the
// URI doesn't match the expected format.
func (s SpaceRecordURI) Repo() syntax.DID {
	parts := spaceRecordURIPartsRegex.FindStringSubmatch(string(s))
	if len(parts) < 7 {
		return ""
	}
	did, err := syntax.ParseDID(parts[4])
	if err != nil {
		return ""
	}
	return did
}

// Rkey extracts the record key from the URI, i.e.
// "{spaceURI}/{repo}/{collection}/{rkey}" -> {rkey}. Returns "" if the URI
// doesn't match the expected format.
func (s SpaceRecordURI) Rkey() syntax.RecordKey {
	parts := spaceRecordURIPartsRegex.FindStringSubmatch(string(s))
	if len(parts) < 7 {
		return ""
	}
	rkey, err := syntax.ParseRecordKey(parts[6])
	if err != nil {
		return ""
	}
	return rkey
}
