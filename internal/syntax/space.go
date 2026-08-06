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

// SpaceURI identifies a space, in the proposal 0016 format:
// "at://spaceDID/space/spaceType/skey"
type SpaceURI string

var spaceURIRegex = regexp.MustCompile(
	`^at:\/\/(?P<did>[a-zA-Z0-9._:%-]+)\/space\/(?P<type>[a-zA-Z0-9-.]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})$`,
)

func ConstructSpaceURI(spaceDID syntax.DID, spaceType syntax.NSID, skey SpaceKey) SpaceURI {
	return SpaceURI(fmt.Sprintf("at://%s/space/%s/%s", spaceDID, spaceType, skey))
}

func ParseSpaceURI(raw string) (SpaceURI, error) {
	if len(raw) > 8192 {
		return "", errors.New("SpaceURI is too long (8192 chars max)")
	}
	parts := spaceURIRegex.FindStringSubmatch(raw)
	if parts == nil {
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

func (s SpaceURI) parse() (did, nsid, skey string) {
	if parts := spaceURIRegex.FindStringSubmatch(string(s)); parts != nil {
		return parts[1], parts[2], parts[3]
	}
	return "", "", ""
}

func (s SpaceURI) SpaceOwner() syntax.DID {
	didStr, _, _ := s.parse()
	if didStr == "" {
		return ""
	}
	did, err := syntax.ParseDID(didStr)
	if err != nil {
		return ""
	}
	return did
}

func (s SpaceURI) SpaceType() syntax.NSID {
	_, nsidStr, _ := s.parse()
	if nsidStr == "" {
		return ""
	}
	nsid, err := syntax.ParseNSID(nsidStr)
	if err != nil {
		return ""
	}
	return nsid
}

func (s SpaceURI) Skey() SpaceKey {
	_, _, skeyStr := s.parse()
	return SpaceKey(skeyStr)
}

func (s SpaceURI) String() string {
	return string(s)
}

type SpaceRecordURI string

// spaceRecordURIRegex matches: at://{did}/space/{type}/{skey}/{repo}/{collection}/{rkey}
var spaceRecordURIRegex = regexp.MustCompile(
	`^at:\/\/(?P<did>[a-zA-Z0-9._:%-]+)\/space\/(?P<type>[a-zA-Z0-9-.]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})` +
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

// spaceRecordURIParts holds the raw, unvalidated components of a
// SpaceRecordURI. All fields are "" when the URI is malformed.
type spaceRecordURIParts struct {
	did        string
	nsid       string
	skey       string
	repo       string
	collection string
	rkey       string
}

func (s SpaceRecordURI) parse() spaceRecordURIParts {
	parts := spaceRecordURIRegex.FindStringSubmatch(string(s))
	if parts == nil {
		return spaceRecordURIParts{}
	}
	return spaceRecordURIParts{
		did:        parts[1],
		nsid:       parts[2],
		skey:       parts[3],
		repo:       parts[4],
		collection: parts[5],
		rkey:       parts[6],
	}
}

// Collection extracts the NSID of the record's collection from the URI.
// Returns "" if the URI doesn't match the expected format.
func (s SpaceRecordURI) Collection() syntax.NSID {
	parts := s.parse()
	if parts.collection == "" {
		return ""
	}
	nsid, err := syntax.ParseNSID(parts.collection)
	if err != nil {
		return ""
	}
	return nsid
}

// SpaceURI extracts the SpaceURI prefix of a SpaceRecordURI.
// Returns "" if the URI doesn't match the expected format.
func (s SpaceRecordURI) SpaceURI() SpaceURI {
	parts := s.parse()
	if parts.did == "" {
		return ""
	}
	spaceURI, err := ParseSpaceURI(
		fmt.Sprintf("at://%s/space/%s/%s", parts.did, parts.nsid, parts.skey),
	)
	if err != nil {
		return ""
	}
	return spaceURI
}

// SpaceOwner extracts the DID of the owning space's owner from a
// SpaceRecordURI, equivalent to s.SpaceURI().SpaceOwner(). Returns "" if the
// URI doesn't match the expected format.
func (s SpaceRecordURI) SpaceOwner() syntax.DID {
	return s.SpaceURI().SpaceOwner()
}

// Repo extracts the DID of the repo that owns the record from the URI.
// Returns "" if the URI doesn't match the expected format.
func (s SpaceRecordURI) Repo() syntax.DID {
	parts := s.parse()
	if parts.repo == "" {
		return ""
	}
	did, err := syntax.ParseDID(parts.repo)
	if err != nil {
		return ""
	}
	return did
}

// Rkey extracts the record key from the URI.
// Returns "" if the URI doesn't match the expected format.
func (s SpaceRecordURI) Rkey() syntax.RecordKey {
	parts := s.parse()
	if parts.rkey == "" {
		return ""
	}
	rkey, err := syntax.ParseRecordKey(parts.rkey)
	if err != nil {
		return ""
	}
	return rkey
}
