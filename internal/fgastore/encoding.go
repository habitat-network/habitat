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

// OrgObjectKey returns the FGA object key for an organization.
func OrgObjectKey(org syntax.DID) string {
	return "organization:" + url.QueryEscape(org.String())
}

// GroupObjectKey returns the FGA object key for a group, keyed by the group
// record's SpaceRecordURI.
func GroupObjectKey(uri habitat_syntax.SpaceRecordURI) string {
	return "group:" + url.QueryEscape(uri.String())
}

// GroupMemberUserString returns the FGA userset string for "all members of a
// group", used when a group is the subject of a relationship tuple.
func GroupMemberUserString(uri habitat_syntax.SpaceRecordURI) string {
	return GroupObjectKey(uri) + "#" + RelationGroupMember
}

// SpaceRoleUserString returns the FGA userset string for "all holders of a role
// on a space", used for cross-space inheritance.
func SpaceRoleUserString(uri habitat_syntax.SpaceURI, fgaRelation string) string {
	return SpaceObjectKey(uri) + "#" + fgaRelation
}

// OrgRoleUserString returns the FGA userset string for "all holders of a role in
// an org" (admin or member), used to assign a whole org to a space role.
func OrgRoleUserString(org syntax.DID, fgaRelation string) string {
	return OrgObjectKey(org) + "#" + fgaRelation
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

// ParseGroupObjectKey parses an FGA group object key back into a SpaceRecordURI.
func ParseGroupObjectKey(key string) (habitat_syntax.SpaceRecordURI, error) {
	if !strings.HasPrefix(key, "group:") {
		return "", fmt.Errorf("invalid group object key: %s", key)
	}
	raw, err := url.QueryUnescape(strings.TrimPrefix(key, "group:"))
	if err != nil {
		return "", fmt.Errorf("parse group object key: %w", err)
	}
	return habitat_syntax.SpaceRecordURI(raw), nil
}
