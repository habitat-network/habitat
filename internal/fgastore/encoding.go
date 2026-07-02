package fgastore

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// OpenFGA uses ":" as a delimiter in user and object references,
// but DIDs and SpaceURIs contain ":" (e.g. "did:plc:alice").  We use
// URL encoding (%3A for ":") to safely encode them into FGA tuple strings.

// SpaceObjectKey returns the FGA object key for a space.
// The key is the full SpaceURI URL-encoded so OpenFGA can parse it as a typed object.
func SpaceObjectKey(uri habitat_syntax.SpaceURI) string {
	return "space:" + url.QueryEscape(uri.String())
}

// MemberUserString returns the FGA user string for a DID member.
func MemberUserString(did syntax.DID) string {
	return "user:" + url.QueryEscape(did.String())
}

// OrgObjectKey returns the FGA object key for an org.
// The key uses the DID URL-encoded so OpenFGA can parse it as a typed object.
func OrgObjectKey(did syntax.DID) string {
	return "organization:" + url.QueryEscape(did.String())
}

// OrgMemberUsersetString returns the FGA userset string for all members of an
// organization, e.g. "organization:<did>#member". This can be used as a user
// in tuples to grant all org members a relation on another object.
func OrgMemberUsersetString(did syntax.DID) string {
	return OrgObjectKey(did) + "#" + RelationMember
}

// OrgMemberContextualTuple returns a Tuple granting org members (via the
// organization:#member userset) the can_read relation on the org's self space
// (ats://<org>/network.habitat.organization/self).  This lets org membership
// chain through stored tuples like "self#reader → can_read → <space>" without
// storing per-member tuples on every space.
func OrgMemberContextualTuple(org syntax.DID) Tuple {
	selfSpace := habitat_syntax.ConstructSpaceURI(
		org,
		"network.habitat.organization",
		habitat_syntax.SpaceKey("self"),
	)
	return Tuple{
		User:     OrgMemberUsersetString(org),
		Relation: RelationSpaceReader,
		Object:   SpaceObjectKey(selfSpace),
	}
}

// SpaceUsersetString returns the FGA userset string for all subjects holding
// `relation` on the given space, e.g. "space:<spaceURI>#can_read". This is how a
// space (including a group-space) is referenced as a grantee on another space.
func SpaceUsersetString(uri habitat_syntax.SpaceURI, relation string) string {
	return SpaceObjectKey(uri) + "#" + relation
}

// MemberUserToDID extracts a DID from an FGA user string.
func MemberUserToDID(user string) (syntax.DID, error) {
	if !strings.HasPrefix(user, "user:") {
		return "", fmt.Errorf("invalid fga user format: %s", user)
	}
	raw, err := url.QueryUnescape(strings.TrimPrefix(user, "user:"))
	if err != nil {
		return "", fmt.Errorf("member user to did: %w", err)
	}
	return syntax.ParseDID(raw)
}

// ParseSpaceObjectKey parses an FGA space object key back into a SpaceURI.
func ParseSpaceObjectKey(key string) (habitat_syntax.SpaceURI, error) {
	if !strings.HasPrefix(key, "space:") {
		return "", fmt.Errorf("invalid space object key: %s", key)
	}
	raw, err := url.QueryUnescape(strings.TrimPrefix(key, "space:"))
	if err != nil {
		return "", fmt.Errorf("parse space object key: %w", err)
	}
	return habitat_syntax.ParseSpaceURI(raw)
}
