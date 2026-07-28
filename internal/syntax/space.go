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
// New format: "at://spaceDID/space/spaceType/skey"
// Legacy format (still parsed): "ats://spaceDID/spaceType/skey"
type SpaceURI string

// newSpaceURIRegex matches the proposal 0016 format: at://{did}/space/{type}/{skey}
var newSpaceURIRegex = regexp.MustCompile(
	`^at:\/\/(?P<did>[a-zA-Z0-9._:%-]+)\/space\/(?P<type>[a-zA-Z0-9-.]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})$`,
)

// legacySpaceURIRegex matches the old format: ats://{did}/{type}/{skey}
var legacySpaceURIRegex = regexp.MustCompile(
	`^ats:\/\/(?P<did>[a-zA-Z0-9._:%-]+)\/(?P<type>[a-zA-Z0-9-.]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})$`,
)

// ConstructSpaceURI returns a space URI in the proposal 0016 format:
// at://{spaceDid}/space/{spaceType}/{skey}
func ConstructSpaceURI(spaceDID syntax.DID, spaceType syntax.NSID, skey SpaceKey) SpaceURI {
	return SpaceURI(fmt.Sprintf("at://%s/space/%s/%s", spaceDID, spaceType, skey))
}

func ParseSpaceURI(raw string) (SpaceURI, error) {
	if len(raw) > 8192 {
		return "", errors.New("SpaceURI is too long (8192 chars max)")
	}
	// Try new format first, then legacy.
	parts := newSpaceURIRegex.FindStringSubmatch(raw)
	if parts == nil {
		parts = legacySpaceURIRegex.FindStringSubmatch(raw)
	}
	if parts == nil || parts[0] == "" {
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
	if parts := newSpaceURIRegex.FindStringSubmatch(string(s)); parts != nil {
		return parts[1], parts[2], parts[3]
	}
	if parts := legacySpaceURIRegex.FindStringSubmatch(string(s)); parts != nil {
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

// newSpaceRecordURIRegex matches: at://{did}/space/{type}/{skey}/{repo}/{collection}/{rkey}
var newSpaceRecordURIRegex = regexp.MustCompile(
	`^at:\/\/(?P<did>[a-zA-Z0-9._:%-]+)\/space\/(?P<type>[a-zA-Z0-9-.]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})` +
		`\/(?P<repo>[a-zA-Z0-9._:%-]+)\/(?P<collection>[a-zA-Z0-9-.]+)\/(?P<rkey>[a-zA-Z0-9_~.:-]{1,512})$`,
)

// legacySpaceRecordURIRegex matches: ats://{did}/{type}/{skey}/{repo}/{collection}/{rkey}
var legacySpaceRecordURIRegex = regexp.MustCompile(
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

func (s SpaceRecordURI) parse() (did, nsid, skey, repo, collection, rkey string) {
	if parts := newSpaceRecordURIRegex.FindStringSubmatch(string(s)); parts != nil {
		return parts[1], parts[2], parts[3], parts[4], parts[5], parts[6]
	}
	if parts := legacySpaceRecordURIRegex.FindStringSubmatch(string(s)); parts != nil {
		return parts[1], parts[2], parts[3], parts[4], parts[5], parts[6]
	}
	return "", "", "", "", "", ""
}

// Collection extracts the NSID of the record's collection from the URI.
// Returns "" if the URI doesn't match the expected format.
func (s SpaceRecordURI) Collection() syntax.NSID {
	_, _, _, _, collStr, _ := s.parse()
	if collStr == "" {
		return ""
	}
	nsid, err := syntax.ParseNSID(collStr)
	if err != nil {
		return ""
	}
	return nsid
}

// SpaceURI extracts the SpaceURI prefix of a SpaceRecordURI.
// Returns "" if the URI doesn't match the expected format.
func (s SpaceRecordURI) SpaceURI() SpaceURI {
	didStr, nsidStr, skeyStr, _, _, _ := s.parse()
	if didStr == "" {
		return ""
	}
	spaceURI, err := ParseSpaceURI(fmt.Sprintf("at://%s/space/%s/%s", didStr, nsidStr, skeyStr))
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
	_, _, _, repoStr, _, _ := s.parse()
	if repoStr == "" {
		return ""
	}
	did, err := syntax.ParseDID(repoStr)
	if err != nil {
		return ""
	}
	return did
}

// Rkey extracts the record key from the URI.
// Returns "" if the URI doesn't match the expected format.
func (s SpaceRecordURI) Rkey() syntax.RecordKey {
	_, _, _, _, _, rkeyStr := s.parse()
	if rkeyStr == "" {
		return ""
	}
	rkey, err := syntax.ParseRecordKey(rkeyStr)
	if err != nil {
		return ""
	}
	return rkey
}
