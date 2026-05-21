package fgastore

import (
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// OpenFGA uses ":" as a delimiter in user and object references,
// but DIDs contain ":" (e.g. "did:plc:alice").  We use "_" as a
// stand-in for ":" when encoding DIDs into FGA tuple strings, and
// reverse the substitution when parsing them back.

func fgaEncodeDID(did syntax.DID) string {
	return strings.ReplaceAll(did.String(), ":", "_")
}

func fgaDecodeDID(s string) (syntax.DID, error) {
	return syntax.ParseDID(strings.ReplaceAll(s, "_", ":"))
}

// SpaceObjectKey returns the FGA object key for a space.
// The key is the full SpaceURI with colons replaced by underscores
// so OpenFGA can parse it as a typed object.
func SpaceObjectKey(uri habitat_syntax.SpaceURI) string {
	return "space:" + strings.ReplaceAll(uri.String(), ":", "_")
}

// MemberUserString returns the FGA user string for a DID member.
func MemberUserString(did syntax.DID) string {
	return "user:" + fgaEncodeDID(did)
}

// MemberUserToDID extracts a DID from an FGA user string.
func MemberUserToDID(user string) (syntax.DID, error) {
	if !strings.HasPrefix(user, "user:") {
		return "", fmt.Errorf("invalid fga user format: %s", user)
	}
	return fgaDecodeDID(strings.TrimPrefix(user, "user:"))
}

// ParseSpaceObjectKey parses an FGA space object key back into a SpaceURI.
func ParseSpaceObjectKey(key string) (habitat_syntax.SpaceURI, error) {
	if !strings.HasPrefix(key, "space:") {
		return "", fmt.Errorf("invalid space object key: %s", key)
	}
	raw := strings.ReplaceAll(strings.TrimPrefix(key, "space:"), "_", ":")
	return habitat_syntax.ParseSpaceURI(raw)
}
