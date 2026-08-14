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
// "at://spaceDID/space/spaceType/skey".
// The pre-0016 format "ats://spaceDID/spaceType/skey" is still accepted when
// parsing, so URIs held by older clients and stored data keep resolving, but is
// only produced by [SpaceURI.Legacy].
type SpaceURI string

var spaceURIRegex = regexp.MustCompile(
	`^at:\/\/(?P<did>[a-zA-Z0-9._:%-]+)\/space\/(?P<type>[a-zA-Z0-9-.]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})$`,
)

var legacySpaceURIRegex = regexp.MustCompile(
	`^ats:\/\/(?P<did>[a-zA-Z0-9._:%-]+)\/(?P<type>[a-zA-Z0-9-.]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})$`,
)

func ConstructSpaceURI(spaceDID syntax.DID, spaceType syntax.NSID, skey SpaceKey) SpaceURI {
	return SpaceURI(fmt.Sprintf("at://%s/space/%s/%s", spaceDID, spaceType, skey))
}

// ParseSpaceURI validates raw as a space URI in either the current or the
// pre-0016 format, and returns it in the current format: a legacy URI from an
// older client or from stored data is normalized, so parsed URIs compare equal
// to constructed ones and match the URIs held in the spaces tables.
func ParseSpaceURI(raw string) (SpaceURI, error) {
	if len(raw) > 8192 {
		return "", errors.New("SpaceURI is too long (8192 chars max)")
	}
	parts := spaceURIRegex.FindStringSubmatch(raw)
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
	return SpaceURI(raw).Canonical(), nil
}

func (s SpaceURI) parse() (did, nsid, skey string) {
	for _, re := range []*regexp.Regexp{spaceURIRegex, legacySpaceURIRegex} {
		if parts := re.FindStringSubmatch(string(s)); parts != nil {
			return parts[1], parts[2], parts[3]
		}
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

func (s SpaceURI) URI() syntax.URI {
	return syntax.URI(s)
}

// Canonical returns the URI in the current proposal 0016 format
// ("at://<did>/space/<type>/<skey>"), converting a legacy URI. URIs that match
// neither format are returned unchanged.
func (s SpaceURI) Canonical() SpaceURI {
	did, nsid, skey := s.parse()
	if did == "" {
		return s
	}
	return SpaceURI(fmt.Sprintf("at://%s/space/%s/%s", did, nsid, skey))
}

// Legacy returns the URI in the pre-0016 format ("ats://<did>/<type>/<skey>").
// It is used where an encoding must stay byte-compatible with data written
// before the 0016 migration — notably FGA object keys, whose stored tuples are
// keyed on the URI string (see fgastore.SpaceObjectKey). URIs that match
// neither format are returned unchanged.
func (s SpaceURI) Legacy() SpaceURI {
	did, nsid, skey := s.parse()
	if did == "" {
		return s
	}
	return SpaceURI(fmt.Sprintf("ats://%s/%s/%s", did, nsid, skey))
}

type SpaceRecordURI string

// spaceRecordURIRegex matches: at://{did}/space/{type}/{skey}/{repo}/{collection}/{rkey}
var spaceRecordURIRegex = regexp.MustCompile(
	`^at:\/\/(?P<did>[a-zA-Z0-9._:%-]+)\/space\/(?P<type>[a-zA-Z0-9-.]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})` +
		`\/(?P<repo>[a-zA-Z0-9._:%-]+)\/(?P<collection>[a-zA-Z0-9-.]+)\/(?P<rkey>[a-zA-Z0-9_~.:-]{1,512})$`,
)

// legacySpaceRecordURIRegex matches the pre-0016 form:
// ats://{did}/{type}/{skey}/{repo}/{collection}/{rkey}
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
	for _, re := range []*regexp.Regexp{spaceRecordURIRegex, legacySpaceRecordURIRegex} {
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

// SpaceURI extracts the SpaceURI prefix of a SpaceRecordURI, always in the
// current at:// format: a legacy record URI yields a current-format space URI.
// Returns "" if the URI doesn't match either format.
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
