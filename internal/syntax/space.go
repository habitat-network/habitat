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

// Legacy returns the pre-proposal-0016 "ats://{did}/{type}/{skey}" form of the
// URI. Identifiers that were persisted before the at:// migration (notably FGA
// object keys, see fgastore.SpaceObjectKey) are still written in this form, so
// that a space addressed in either format resolves to the same stored data.
// Returns "" if the URI doesn't match either format.
func (s SpaceURI) Legacy() SpaceURI {
	did, nsid, skey := s.parse()
	if did == "" {
		return ""
	}
	return SpaceURI(fmt.Sprintf("ats://%s/%s/%s", did, nsid, skey))
}

// Canonical returns the proposal 0016 "at://{did}/space/{type}/{skey}" form of
// the URI. This is the format spaces are handed back to clients in. Returns ""
// if the URI doesn't match either format.
func (s SpaceURI) Canonical() SpaceURI {
	did, nsid, skey := s.parse()
	if did == "" {
		return ""
	}
	return SpaceURI(fmt.Sprintf("at://%s/space/%s/%s", did, nsid, skey))
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

// spaceRecordURIParts holds the raw, unvalidated components of a
// SpaceRecordURI. All fields are "" when the URI matches neither format.
type spaceRecordURIParts struct {
	did        string
	nsid       string
	skey       string
	repo       string
	collection string
	rkey       string
}

func (s SpaceRecordURI) parse() spaceRecordURIParts {
	for _, re := range []*regexp.Regexp{newSpaceRecordURIRegex, legacySpaceRecordURIRegex} {
		parts := re.FindStringSubmatch(string(s))
		if parts == nil {
			continue
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
	return spaceRecordURIParts{}
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
