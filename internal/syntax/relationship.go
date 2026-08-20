package syntax

import "github.com/bluesky-social/indigo/atproto/syntax"

// SpaceRole is an access-control role held on a space. The hierarchy
// (owner ⇒ manager ⇒ writer ⇒ reader) is enforced by the OpenFGA model
// (internal/fgastore's authModel), not by this package.
type SpaceRole string

const (
	SpaceRoleOwner   SpaceRole = "owner"
	SpaceRoleManager SpaceRole = "manager"
	SpaceRoleWriter  SpaceRole = "writer"
	SpaceRoleReader  SpaceRole = "reader"
)

const (
	UserRelationCollection  syntax.NSID = "network.habitat.relationship.userRelation"
	SpaceRelationCollection syntax.NSID = "network.habitat.relationship.spaceRelation"
)
