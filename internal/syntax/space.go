package syntax

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
)

type Skey string

func NewSkey() Skey {
	return Skey(uuid.New().String())
}

// SpaceURI identifies a space.
// Format: "ats://spaceDID/spaceType/skey"
type SpaceURI string

var spaceURIRegex = regexp.MustCompile(
	`^ats:\/\/(?P<did>[a-zA-Z0-9._:%-]+)\/(?P<type>[a-zA-Z0-9-.]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})$`,
)

func ConstructSpaceURI(spaceDID syntax.DID, spaceType syntax.NSID, skey Skey) SpaceURI {
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

func (s SpaceURI) SpaceDID() syntax.DID {
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

func (s SpaceURI) Skey() Skey {
	parts := spaceURIRegex.FindStringSubmatch(string(s))
	if len(parts) < 4 {
		return ""
	}
	return Skey(parts[3])
}

func (s SpaceURI) String() string {
	return string(s)
}
