package syntax

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

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

// ParseSpaceRecordURI parses a SpaceRecordURI of the form
// "ats://did/type/skey/repo/collection/rkey" back into its components.
func ParseSpaceRecordURI(
	raw string,
) (uri SpaceRecordURI, space SpaceURI, repo syntax.DID, collection syntax.NSID, rkey syntax.RecordKey, err error) {
	rest, ok := strings.CutPrefix(raw, "ats://")
	if !ok {
		err = errors.New("invalid space record URI: missing ats:// scheme")
		return
	}
	// did/type/skey/repo/collection/rkey — none of the components contain "/".
	parts := strings.Split(rest, "/")
	if len(parts) != 6 {
		err = errors.New("invalid space record URI: expected 6 path segments")
		return
	}
	space, err = ParseSpaceURI(fmt.Sprintf("ats://%s/%s/%s", parts[0], parts[1], parts[2]))
	if err != nil {
		err = fmt.Errorf("invalid space record URI space: %w", err)
		return
	}
	repo, err = syntax.ParseDID(parts[3])
	if err != nil {
		err = fmt.Errorf("invalid space record URI repo: %w", err)
		return
	}
	collection, err = syntax.ParseNSID(parts[4])
	if err != nil {
		err = fmt.Errorf("invalid space record URI collection: %w", err)
		return
	}
	rkey, err = syntax.ParseRecordKey(parts[5])
	if err != nil {
		err = fmt.Errorf("invalid space record URI rkey: %w", err)
		return
	}
	uri = SpaceRecordURI(raw)
	return
}
