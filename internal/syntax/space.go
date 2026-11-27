package syntax

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
)

// SpaceKey is a space's key, a record key as indigo defines it.
type SpaceKey = syntax.RecordKey

func NewSkey(tid syntax.TID) SpaceKey {
	return SpaceKey(tid)
}

func ParseSkey(s string) (SpaceKey, error) {
	return syntax.ParseRecordKey(s)
}

// SpaceURI identifies a space, in the proposal 0016 format pear stores:
// "at://spaceDID/space/spaceType/skey".
//
// Validation and splitting are done by indigo's [syntax.SpaceRef] and
// [syntax.SpaceURI], which only know the "at:space/..." form. This type adds
// the other formats pear accepts, converting them to that form first:
//   - "at:space/spaceDID/spaceType/skey", the form the spaces alpha moved to
//     (bluesky-social/atproto permissioned-data-alpha, 2026-11-24). Alpha PDSes
//     now send and require it; produce it with [SpaceURI.AtSpace] when calling
//     one.
//   - the pre-0016 format "ats://spaceDID/spaceType/skey", so URIs held by
//     older clients and stored data keep resolving; produced only by
//     [SpaceURI.Legacy].
//
// Parsing normalizes every accepted format to the current "at://" one.
type SpaceURI string

// toAtSpace rewrites a space (or space record) URI in any accepted format to
// the "at:space/..." form indigo parses. ok is false for a string that isn't
// shaped like any accepted format; it isn't validated further.
func toAtSpace(raw string) (atSpace string, ok bool) {
	switch {
	case strings.HasPrefix(raw, "at:space/"):
		return raw, true
	case strings.HasPrefix(raw, "ats://"):
		return "at:space/" + strings.TrimPrefix(raw, "ats://"), true
	case strings.HasPrefix(raw, "at://"):
		did, rest, _ := strings.Cut(strings.TrimPrefix(raw, "at://"), "/")
		if after, found := strings.CutPrefix(rest, "space/"); found {
			return "at:space/" + did + "/" + after, true
		}
	}
	return "", false
}

func ConstructSpaceURI(spaceDID syntax.DID, spaceType syntax.NSID, skey SpaceKey) SpaceURI {
	return SpaceURI(fmt.Sprintf("at://%s/space/%s/%s", spaceDID, spaceType, skey))
}

// spaceURIFromRef is the current-format URI for an indigo space ref.
func spaceURIFromRef(ref syntax.SpaceRef) SpaceURI {
	return ConstructSpaceURI(ref.SpaceAuthority(), ref.SpaceType(), ref.SpaceKey())
}

// ParseSpaceURI validates raw as a space URI in any accepted format (see
// [SpaceURI]), and returns it in the current format: a URI in another format,
// from a client, a PDS or stored data, is normalized, so parsed URIs compare
// equal to constructed ones and match the URIs held in the spaces tables.
func ParseSpaceURI(raw string) (SpaceURI, error) {
	if len(raw) > 8192 {
		return "", errors.New("SpaceURI is too long (8192 chars max)")
	}
	atSpace, ok := toAtSpace(raw)
	if !ok {
		return "", errors.New("invalid space URI format")
	}
	ref, err := syntax.ParseSpaceRef(atSpace)
	if err != nil {
		return "", fmt.Errorf("invalid space URI: %w", err)
	}
	return spaceURIFromRef(ref), nil
}

// ref views the URI as an indigo space ref without validating it: indigo's
// accessors parse each part on its own, so an invalid part empties only that
// accessor. ok is false if the URI isn't shaped like a space URI.
func (s SpaceURI) ref() (ref syntax.SpaceRef, ok bool) {
	atSpace, ok := toAtSpace(string(s))
	if !ok || strings.Count(atSpace, "/") != 3 {
		return "", false
	}
	return syntax.SpaceRef(atSpace), true
}

// SpaceOwner returns the space authority's DID, or "" if the URI is invalid.
func (s SpaceURI) SpaceOwner() syntax.DID {
	ref, _ := s.ref()
	return ref.SpaceAuthority()
}

func (s SpaceURI) SpaceType() syntax.NSID {
	ref, _ := s.ref()
	return ref.SpaceType()
}

func (s SpaceURI) Skey() SpaceKey {
	ref, _ := s.ref()
	return ref.SpaceKey()
}

func (s SpaceURI) String() string {
	return string(s)
}

func (s SpaceURI) URI() syntax.URI {
	return syntax.URI(s)
}

// Canonical returns the URI in the current proposal 0016 format
// ("at://<did>/space/<type>/<skey>"), converting another format. URIs that
// aren't valid space URIs are returned unchanged.
func (s SpaceURI) Canonical() SpaceURI {
	ref, ok := s.ref()
	if !ok {
		return s
	}
	return spaceURIFromRef(ref)
}

// AtSpace returns the URI in the "at:space/<did>/<type>/<skey>" form that
// spaces alpha PDSes require (see [SpaceURI]), which is indigo's
// [syntax.SpaceRef]. Use it for space URIs sent to a PDS. URIs that aren't
// valid space URIs are returned unchanged.
func (s SpaceURI) AtSpace() SpaceURI {
	ref, ok := s.ref()
	if !ok {
		return s
	}
	return SpaceURI(ref)
}

// Legacy returns the URI in the pre-0016 format ("ats://<did>/<type>/<skey>").
// It is used where an encoding must stay byte-compatible with data written
// before the 0016 migration — notably FGA object keys, whose stored tuples are
// keyed on the URI string (see fgastore.SpaceObjectKey). URIs that aren't
// valid space URIs are returned unchanged.
func (s SpaceURI) Legacy() SpaceURI {
	ref, ok := s.ref()
	if !ok {
		return s
	}
	return SpaceURI(fmt.Sprintf(
		"ats://%s/%s/%s", ref.SpaceAuthority(), ref.SpaceType(), ref.SpaceKey(),
	))
}

// SpaceRecordURI identifies a record in a space: the space URI followed by
// "/{repo}/{collection}/{rkey}", in the current format
// "at://{did}/space/{type}/{skey}/{repo}/{collection}/{rkey}". As with
// [SpaceURI], the "at:space/..." and "ats://..." forms are accepted and parsed
// with indigo's [syntax.SpaceURI].
type SpaceRecordURI string

func ConstructSpaceRecordURI(
	spaceUri SpaceURI,
	repo syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) SpaceRecordURI {
	return SpaceRecordURI(fmt.Sprintf("%s/%s/%s/%s", spaceUri, repo, collection, rkey))
}

// parseRecordURI parses a record URI in any accepted format with indigo,
// requiring the repo, collection and record key indigo leaves optional.
func parseRecordURI(raw string) (syntax.SpaceURI, error) {
	atSpace, ok := toAtSpace(raw)
	if !ok {
		return "", errors.New("invalid space record URI format")
	}
	uri, err := syntax.ParseSpaceURI(atSpace)
	if err != nil {
		return "", err
	}
	if uri.Repo() == "" || uri.Collection() == "" || uri.RecordKey() == "" {
		return "", errors.New("missing repo, collection or record key")
	}
	return uri, nil
}

// ParseSpaceRecordURI validates raw as a space record URI in any accepted
// format (see [SpaceURI]), and returns it in the current format, so parsed
// URIs compare equal to constructed ones.
func ParseSpaceRecordURI(raw string) (SpaceRecordURI, error) {
	uri, err := parseRecordURI(raw)
	if err != nil {
		return "", fmt.Errorf("invalid space record URI: %w", err)
	}
	return recordURIFromIndigo(uri), nil
}

func recordURIFromIndigo(uri syntax.SpaceURI) SpaceRecordURI {
	return ConstructSpaceRecordURI(
		spaceURIFromRef(uri.SpaceRef()), uri.Repo(), uri.Collection(), uri.RecordKey(),
	)
}

func (s SpaceRecordURI) String() string {
	return string(s)
}

// indigo views the URI as an indigo space URI without validating its parts
// (see [SpaceURI.ref]). ok is false if it isn't shaped like a space record URI.
func (s SpaceRecordURI) indigo() (syntax.SpaceURI, bool) {
	atSpace, ok := toAtSpace(string(s))
	if !ok || strings.Count(atSpace, "/") != 6 {
		return "", false
	}
	return syntax.SpaceURI(atSpace), true
}

// AtSpace returns the URI in the "at:space/..." form that spaces alpha PDSes
// require (see [SpaceURI.AtSpace]). URIs that aren't valid are returned
// unchanged.
func (s SpaceRecordURI) AtSpace() SpaceRecordURI {
	uri, ok := s.indigo()
	if !ok {
		return s
	}
	return SpaceRecordURI(uri)
}

// Collection extracts the NSID of the record's collection from the URI.
// Returns "" if the URI isn't valid.
func (s SpaceRecordURI) Collection() syntax.NSID {
	uri, _ := s.indigo()
	return uri.Collection()
}

// SpaceURI extracts the SpaceURI prefix of a SpaceRecordURI, always in the
// current at:// format: a legacy record URI yields a current-format space URI.
// Returns "" if the URI isn't valid.
func (s SpaceRecordURI) SpaceURI() SpaceURI {
	uri, ok := s.indigo()
	if !ok {
		return ""
	}
	ref, err := syntax.ParseSpaceRef(string(uri.SpaceRef()))
	if err != nil {
		return ""
	}
	return spaceURIFromRef(ref)
}

// SpaceOwner extracts the DID of the owning space's owner from a
// SpaceRecordURI, equivalent to s.SpaceURI().SpaceOwner(). Returns "" if the
// URI isn't valid.
func (s SpaceRecordURI) SpaceOwner() syntax.DID {
	uri, _ := s.indigo()
	return uri.SpaceAuthority()
}

// Repo extracts the DID of the repo that owns the record from the URI.
// Returns "" if the URI isn't valid.
func (s SpaceRecordURI) Repo() syntax.DID {
	uri, _ := s.indigo()
	return uri.Repo()
}

// Rkey extracts the record key from the URI.
// Returns "" if the URI isn't valid.
func (s SpaceRecordURI) Rkey() syntax.RecordKey {
	uri, _ := s.indigo()
	return uri.RecordKey()
}

// SpaceRef is a space URI split into its parts, for storing in a table's
// columns instead of the URI string. Embed it in a GORM model with
// `gorm:"embedded;embeddedPrefix:space_"` for the columns space_owner,
// space_type and space_skey.
//
// The fields are tagged as primary key columns: every table that embeds a
// SpaceRef is keyed on the space first.
type SpaceRef struct {
	Owner syntax.DID  `gorm:"primaryKey"`
	Type  syntax.NSID `gorm:"primaryKey"`
	Skey  SpaceKey    `gorm:"primaryKey"`
}

// Ref splits the URI into its parts. A URI that matches no accepted format
// gives the zero SpaceRef.
func (s SpaceURI) Ref() SpaceRef {
	return SpaceRef{Owner: s.SpaceOwner(), Type: s.SpaceType(), Skey: s.Skey()}
}

// URI joins the parts back into a space URI in the current format. The zero
// SpaceRef gives the empty URI.
func (r SpaceRef) URI() SpaceURI {
	if r == (SpaceRef{}) {
		return ""
	}
	return ConstructSpaceURI(r.Owner, r.Type, r.Skey)
}

// InSpace is a GORM scope that restricts a query to the rows of a space,
// matching on the space_owner, space_type and space_skey columns that
// [SpaceRef] embeds, for tables laid out as the models describe.
func InSpace(uri SpaceURI) func(*gorm.DB) *gorm.DB {
	ref := uri.Ref()
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(
			"space_owner = ? AND space_type = ? AND space_skey = ?",
			ref.Owner, ref.Type, ref.Skey,
		)
	}
}
