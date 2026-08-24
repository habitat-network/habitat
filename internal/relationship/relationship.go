// Package relationship implements the network.habitat.relationship.* XRPC
// surface: reading and writing app-managed ReBAC relations over spaces, and
// checking/expanding them. It is a thin XRPC layer over internal/perms,
// which owns the actual storage (AT Protocol records mirrored into OpenFGA)
// and permission logic.
//
// Groups, and an org's member/admin sets, are modeled as spaces: there is no
// dedicated group or organization concept here. A group is a space (type
// network.habitat.group) and is referenced as a grantee via a space-role
// userset (subject + subjectRole) over that group-space (role reader = "all
// members of the group").
package relationship

import (
	"fmt"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// parseSpaceRole validates a role string against the roles known to the
// lexicon (owner|manager|writer|reader) and converts it to
// habitat_syntax.SpaceRole.
func parseSpaceRole(role string) (habitat_syntax.SpaceRole, error) {
	switch habitat_syntax.SpaceRole(role) {
	case habitat_syntax.SpaceRoleOwner,
		habitat_syntax.SpaceRoleManager,
		habitat_syntax.SpaceRoleWriter,
		habitat_syntax.SpaceRoleReader:
		return habitat_syntax.SpaceRole(role), nil
	default:
		return "", fmt.Errorf("invalid role: %s", role)
	}
}
